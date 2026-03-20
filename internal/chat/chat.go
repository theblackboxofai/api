package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"blackbox-api/internal/models"
)

const (
	ollamaChatCompletionsPath = "/v1/chat/completions"
	blackboxUserAgent         = "Blackbox/1.0"
)

var allowedChatCompletionFields = map[string]struct{}{
	"model":             {},
	"messages":          {},
	"frequency_penalty": {},
	"presence_penalty":  {},
	"repeat_penalty":    {},
	"response_format":   {},
	"seed":              {},
	"stop":              {},
	"stream":            {},
	"stream_options":    {},
	"temperature":       {},
	"top_p":             {},
	"top_k":             {},
	"max_tokens":        {},
	"tools":             {},
	"tool_choice":       {},
	"logit_bias":        {},
	"user":              {},
	"n":                 {},
}

var allowedStreamOptionsFields = map[string]struct{}{
	"include_usage": {},
}

type Repository interface {
	ListCandidateServers(ctx context.Context, rawModelID string) ([]string, error)
	InsertLog(ctx context.Context, entry LogEntry) error
}

type Service struct {
	client *http.Client
	debug  bool
	mapper models.ModelMapper
	repo   Repository
}

type LogEntry struct {
	RequestID       string
	RequestedModel  string
	RawModelID      string
	ServerURL       string
	Stream          bool
	Success         bool
	ResponseStatus  int
	RequestJSON     string
	ResponseHeaders string
	ResponseBody    string
	ErrorText       string
}

type chatCompletionRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type apiError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func NewService(repo Repository, mapper models.ModelMapper) *Service {
	if mapper == nil {
		mapper = models.StaticModelMapper{}
	}

	return &Service{
		client: &http.Client{},
		mapper: mapper,
		repo:   repo,
	}
}

func (s *Service) WithDebug(enabled bool) *Service {
	s.debug = enabled
	return s
}

func (s *Service) HandleCompletions(w http.ResponseWriter, r *http.Request) {
	s.debugf("incoming chat completion request")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	request, err := validateChatCompletionRequest(body)
	if err != nil {
		s.debugf("request validation failed: %v", err)
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if request.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	rawModelID, ok := s.mapper.LookupRaw(request.Model)
	if !ok || !strings.Contains(rawModelID, models.CloudTag) {
		s.debugf("requested model %q not available", request.Model)
		writeError(w, http.StatusNotFound, "invalid_request_error", "requested model is not available")
		return
	}
	s.debugf("resolved model %q to raw %q", request.Model, rawModelID)

	serverURLs, err := s.pickServers(r.Context(), rawModelID)
	if err != nil {
		s.debugf("server selection failed for %q: %v", rawModelID, err)
		if err == errModelUnavailable {
			writeError(w, http.StatusBadRequest, "server_unavailable", "no servers available for requested model")
			return
		}

		writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to select upstream server")
		return
	}

	upstreamBody, err := rewriteRequestModel(body, rawModelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body")
		return
	}

	requestID := newRequestID()
	s.debugf("request %s trying %d candidate servers", requestID, len(serverURLs))
	upstreamResp, serverURL, err := s.doUpstreamRequest(r.Context(), serverURLs, r.Header, body, upstreamBody, requestID, request.Model, rawModelID, request.Stream)
	if err != nil {
		s.debugf("request %s upstream failed: %v", requestID, err)
		writeError(w, http.StatusBadGateway, "upstream_error", "failed to call upstream server")
		return
	}
	defer upstreamResp.Body.Close()
	s.debugf("request %s using server %s status=%d", requestID, serverURL, upstreamResp.StatusCode)

	if strings.HasPrefix(upstreamResp.Header.Get("Content-Type"), "text/event-stream") {
		responseBody, streamErr := proxyStreamResponse(w, upstreamResp, request.Model)
		s.logAttempt(r.Context(), LogEntry{
			RequestID:       requestID,
			RequestedModel:  request.Model,
			RawModelID:      rawModelID,
			ServerURL:       serverURL,
			Stream:          true,
			Success:         streamErr == nil,
			ResponseStatus:  upstreamResp.StatusCode,
			RequestJSON:     string(body),
			ResponseHeaders: marshalHeaders(upstreamResp.Header),
			ResponseBody:    string(responseBody),
			ErrorText:       errorString(streamErr),
		})
		s.debugf("request %s streamed response success=%t bytes=%d", requestID, streamErr == nil, len(responseBody))
		if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
			return
		}
		return
	}

	responseBody, proxyErr := proxyJSONResponse(w, upstreamResp, request.Model)
	s.logAttempt(r.Context(), LogEntry{
		RequestID:       requestID,
		RequestedModel:  request.Model,
		RawModelID:      rawModelID,
		ServerURL:       serverURL,
		Stream:          false,
		Success:         proxyErr == nil && upstreamResp.StatusCode >= 200 && upstreamResp.StatusCode < 300,
		ResponseStatus:  upstreamResp.StatusCode,
		RequestJSON:     string(body),
		ResponseHeaders: marshalHeaders(upstreamResp.Header),
		ResponseBody:    string(responseBody),
		ErrorText:       errorString(proxyErr),
	})
	s.debugf("request %s completed success=%t bytes=%d", requestID, proxyErr == nil && upstreamResp.StatusCode >= 200 && upstreamResp.StatusCode < 300, len(responseBody))
	if proxyErr != nil {
		if upstreamResp.StatusCode >= 200 && upstreamResp.StatusCode < 300 {
			writeError(w, http.StatusBadGateway, "upstream_error", "failed to decode upstream response")
			return
		}

		writeError(w, http.StatusBadGateway, "upstream_error", "failed to decode upstream error")
	}
}

var errModelUnavailable = fmt.Errorf("no servers available")

func (s *Service) pickServers(ctx context.Context, rawModelID string) ([]string, error) {
	servers, err := s.repo.ListCandidateServers(ctx, rawModelID)
	if err != nil {
		return nil, err
	}

	if len(servers) == 0 {
		return nil, errModelUnavailable
	}

	shuffled := shuffleServers(servers)
	s.debugf("selected %d candidate servers for %q", len(shuffled), rawModelID)
	return shuffled, nil
}

func rewriteRequestModel(body []byte, rawModelID string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	payload["model"] = rawModelID

	return json.Marshal(payload)
}

func (s *Service) doUpstreamRequest(ctx context.Context, serverURLs []string, requestHeaders http.Header, requestBody, upstreamBody []byte, requestID, requestedModel, rawModelID string, stream bool) (*http.Response, string, error) {
	var lastErr error

	for i, serverURL := range serverURLs {
		s.debugf("request %s upstream attempt %d/%d server=%s", requestID, i+1, len(serverURLs), serverURL)
		upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(serverURL, ollamaChatCompletionsPath), bytes.NewReader(upstreamBody))
		if err != nil {
			return nil, "", err
		}

		copyRequestHeaders(upstreamReq.Header, requestHeaders)
		upstreamReq.Header.Set("User-Agent", blackboxUserAgent)
		if upstreamReq.Header.Get("Content-Type") == "" {
			upstreamReq.Header.Set("Content-Type", "application/json")
		}

		upstreamResp, err := s.client.Do(upstreamReq)
		if err != nil {
			s.debugf("request %s transport error from %s: %v", requestID, serverURL, err)
			s.logAttempt(ctx, LogEntry{
				RequestID:      requestID,
				RequestedModel: requestedModel,
				RawModelID:     rawModelID,
				ServerURL:      serverURL,
				Stream:         stream,
				Success:        false,
				RequestJSON:    string(requestBody),
				ErrorText:      err.Error(),
			})
			lastErr = err
			continue
		}

		if upstreamResp.StatusCode >= 200 && upstreamResp.StatusCode < 300 {
			s.debugf("request %s successful upstream from %s", requestID, serverURL)
			return upstreamResp, serverURL, nil
		}

		if !shouldRetryStatus(upstreamResp.StatusCode) || i == len(serverURLs)-1 {
			s.debugf("request %s returning non-retryable/final status %d from %s", requestID, upstreamResp.StatusCode, serverURL)
			return upstreamResp, serverURL, nil
		}

		responseBody, readErr := io.ReadAll(upstreamResp.Body)
		upstreamResp.Body.Close()
		s.logAttempt(ctx, LogEntry{
			RequestID:       requestID,
			RequestedModel:  requestedModel,
			RawModelID:      rawModelID,
			ServerURL:       serverURL,
			Stream:          stream,
			Success:         false,
			ResponseStatus:  upstreamResp.StatusCode,
			RequestJSON:     string(requestBody),
			ResponseHeaders: marshalHeaders(upstreamResp.Header),
			ResponseBody:    string(responseBody),
			ErrorText:       errorString(readErr),
		})
		if readErr != nil {
			s.debugf("request %s failed reading retry response from %s: %v", requestID, serverURL, readErr)
			lastErr = readErr
			continue
		}
		s.debugf("request %s retrying after status %d from %s", requestID, upstreamResp.StatusCode, serverURL)
		lastErr = fmt.Errorf("upstream status %d", upstreamResp.StatusCode)
	}

	return nil, "", lastErr
}

func shouldRetryStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusUnprocessableEntity:
		return false
	default:
		return true
	}
}

func shuffleServers(servers []string) []string {
	shuffled := append([]string(nil), servers...)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}

func validateChatCompletionRequest(body []byte) (chatCompletionRequest, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return chatCompletionRequest{}, fmt.Errorf("invalid JSON body")
	}

	for key, value := range payload {
		if _, ok := allowedChatCompletionFields[key]; !ok {
			return chatCompletionRequest{}, fmt.Errorf("unsupported field: %s", key)
		}

		if key == "stream_options" {
			if err := validateStreamOptions(value); err != nil {
				return chatCompletionRequest{}, err
			}
		}
	}

	var request chatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return chatCompletionRequest{}, fmt.Errorf("invalid JSON body")
	}

	return request, nil
}

func validateStreamOptions(body json.RawMessage) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("invalid stream_options")
	}

	for key := range payload {
		if _, ok := allowedStreamOptionsFields[key]; !ok {
			return fmt.Errorf("unsupported stream_options field: %s", key)
		}
	}

	return nil
}

func proxyJSONResponse(w http.ResponseWriter, resp *http.Response, requestedModel string) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	rewritten, err := rewriteResponseModel(body, requestedModel)
	if err != nil {
		return body, err
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(rewritten)
	return rewritten, nil
}

func proxyStreamResponse(w http.ResponseWriter, resp *http.Response, requestedModel string) ([]byte, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported")
	}

	copyResponseHeaders(w.Header(), resp.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	var captured bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			rewritten, rewriteErr := rewriteSSELine(line, requestedModel)
			if rewriteErr != nil {
				return captured.Bytes(), rewriteErr
			}

			captured.WriteString(rewritten)
			if _, writeErr := io.WriteString(w, rewritten); writeErr != nil {
				return captured.Bytes(), writeErr
			}
			flusher.Flush()
		}

		if errors.Is(err, io.EOF) {
			return captured.Bytes(), nil
		}
		if err != nil {
			return captured.Bytes(), err
		}
	}
}

func rewriteSSELine(line, requestedModel string) (string, error) {
	if !strings.HasPrefix(line, "data:") {
		return line, nil
	}

	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return line, nil
	}

	rewritten, err := rewriteResponseModel([]byte(payload), requestedModel)
	if err != nil {
		return "", err
	}

	return "data: " + string(rewritten) + "\n", nil
}

func rewriteResponseModel(body []byte, requestedModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if _, ok := payload["model"]; ok {
		payload["model"] = requestedModel
	}

	return json.Marshal(payload)
}

func copyRequestHeaders(dst, src http.Header) {
	for key, values := range src {
		switch http.CanonicalHeaderKey(key) {
		case "Host", "Content-Length", "User-Agent":
			continue
		}

		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		switch http.CanonicalHeaderKey(key) {
		case "Content-Length", "Transfer-Encoding":
			continue
		}

		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func joinURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func marshalHeaders(headers http.Header) string {
	if len(headers) == 0 {
		return ""
	}

	body, err := json.Marshal(headers)
	if err != nil {
		return ""
	}

	return string(body)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func newRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func (s *Service) logAttempt(ctx context.Context, entry LogEntry) {
	if s.repo == nil {
		return
	}

	s.debugf("log attempt request=%s server=%s success=%t status=%d", entry.RequestID, entry.ServerURL, entry.Success, entry.ResponseStatus)
	_ = s.repo.InsertLog(ctx, entry)
}

func (s *Service) debugf(format string, args ...any) {
	if !s.debug {
		return
	}

	log.Printf("DEBUG chat: "+format, args...)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body.Bytes())
}

func writeError(w http.ResponseWriter, statusCode int, errorType, message string) {
	writeJSON(w, statusCode, apiError{
		Error: errorBody{
			Message: message,
			Type:    errorType,
		},
	})
}
