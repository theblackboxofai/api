package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"blackbox-api/internal/models"
)

func TestHandlerRejectsUnknownModel(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeRepository{}, models.StaticModelMapper{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"unknown/model","messages":[]}`))
	rec := httptest.NewRecorder()

	service.HandleCompletions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandlerRejectsUnsupportedField(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeRepository{}, models.StaticModelMapper{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek/deepseek-v3.2","messages":[],"foo":"bar"}`))
	rec := httptest.NewRecorder()

	service.HandleCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "unsupported field: foo") {
		t.Fatalf("expected unsupported field error, got %q", rec.Body.String())
	}
}

func TestRewriteRequestModel(t *testing.T) {
	t.Parallel()

	body, err := rewriteRequestModel([]byte(`{"model":"deepseek/deepseek-v3.2","stream":false,"messages":[]}`), "deepseek-v3.2:cloud")
	if err != nil {
		t.Fatalf("rewriteRequestModel: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["model"] != "deepseek-v3.2:cloud" {
		t.Fatalf("expected rewritten model, got %#v", payload["model"])
	}

	if payload["stream"] != false {
		t.Fatalf("expected stream=false to be preserved, got %#v", payload["stream"])
	}
}

func TestRewriteRequestModelPreservesTools(t *testing.T) {
	t.Parallel()

	body, err := rewriteRequestModel([]byte(`{
		"model":"deepseek/deepseek-v3.2",
		"messages":[{"role":"user","content":"weather?"}],
		"tool_choice":"auto",
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object"}}}]
	}`), "deepseek-v3.2:cloud")
	if err != nil {
		t.Fatalf("rewriteRequestModel: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["model"] != "deepseek-v3.2:cloud" {
		t.Fatalf("expected rewritten model, got %#v", payload["model"])
	}

	if payload["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice to be preserved, got %#v", payload["tool_choice"])
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one preserved tool, got %#v", payload["tools"])
	}

	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("expected function tool type, got %#v", tool["type"])
	}
}

func TestValidateChatCompletionRequestAllowsTopK(t *testing.T) {
	t.Parallel()

	request, err := validateChatCompletionRequest([]byte(`{"model":"deepseek/deepseek-v3.2","messages":[],"top_k":40}`))
	if err != nil {
		t.Fatalf("expected top_k to be allowed, got error: %v", err)
	}

	if request.Model != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected model to decode, got %q", request.Model)
	}
}

func TestValidateChatCompletionRequestAllowsRepeatPenalty(t *testing.T) {
	t.Parallel()

	request, err := validateChatCompletionRequest([]byte(`{"model":"deepseek/deepseek-v3.2","messages":[],"repeat_penalty":1.1}`))
	if err != nil {
		t.Fatalf("expected repeat_penalty to be allowed, got error: %v", err)
	}

	if request.Model != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected model to decode, got %q", request.Model)
	}
}

func TestValidateStreamOptionsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := validateChatCompletionRequest([]byte(`{"model":"deepseek/deepseek-v3.2","messages":[],"stream_options":{"include_usage":true,"foo":true}}`))
	if err == nil {
		t.Fatal("expected stream_options validation error")
	}

	if err.Error() != "unsupported stream_options field: foo" {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestModelMapperResolvesMappedChatModel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "maps.yml")
	content := []byte("models:\n  \"deepseek-v3.2:cloud\": \"deepseek/deepseek-v3.2\"\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write test map: %v", err)
	}

	mapper, err := models.LoadModelMapper(path)
	if err != nil {
		t.Fatalf("load mapper: %v", err)
	}

	rawID, ok := mapper.LookupRaw("deepseek/deepseek-v3.2")
	if !ok {
		t.Fatal("expected mapped model to resolve to raw id")
	}

	if rawID != "deepseek-v3.2:cloud" {
		t.Fatalf("expected deepseek-v3.2:cloud, got %q", rawID)
	}

	body, err := rewriteRequestModel([]byte(`{"model":"deepseek/deepseek-v3.2","messages":[]}`), rawID)
	if err != nil {
		t.Fatalf("rewriteRequestModel: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["model"] != "deepseek-v3.2:cloud" {
		t.Fatalf("expected raw upstream model, got %#v", payload["model"])
	}
}

func TestRewriteResponseModelPreservesThinkingFields(t *testing.T) {
	t.Parallel()

	body, err := rewriteResponseModel([]byte(`{"id":"chatcmpl-1","model":"deepseek-v3.2:cloud","choices":[{"message":{"role":"assistant","thinking":"step one","content":"answer"}}]}`), "deepseek/deepseek-v3.2")
	if err != nil {
		t.Fatalf("rewriteResponseModel: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["model"] != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected mapped response model, got %#v", payload["model"])
	}

	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["thinking"] != "step one" {
		t.Fatalf("expected thinking field to be preserved, got %#v", message["thinking"])
	}
}

func TestRewriteResponseModelPreservesToolCalls(t *testing.T) {
	t.Parallel()

	body, err := rewriteResponseModel([]byte(`{
		"id":"chatcmpl-1",
		"model":"deepseek-v3.2:cloud",
		"choices":[{
			"message":{
				"role":"assistant",
				"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Austin\"}"}}]
			},
			"finish_reason":"tool_calls"
		}]
	}`), "deepseek/deepseek-v3.2")
	if err != nil {
		t.Fatalf("rewriteResponseModel: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["model"] != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected mapped response model, got %#v", payload["model"])
	}

	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected one preserved tool call, got %#v", message["tool_calls"])
	}

	function := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "get_weather" {
		t.Fatalf("expected tool call function name to be preserved, got %#v", function["name"])
	}
}

func TestRewriteSSELinePreservesThinkingChunks(t *testing.T) {
	t.Parallel()

	line, err := rewriteSSELine("data: {\"id\":\"chatcmpl-1\",\"model\":\"deepseek-v3.2:cloud\",\"choices\":[{\"delta\":{\"thinking\":\"step one\"}}]}\n", "deepseek/deepseek-v3.2")
	if err != nil {
		t.Fatalf("rewriteSSELine: %v", err)
	}

	if !strings.Contains(line, "\"model\":\"deepseek/deepseek-v3.2\"") {
		t.Fatalf("expected rewritten model in SSE line, got %q", line)
	}

	if !strings.Contains(line, "\"thinking\":\"step one\"") {
		t.Fatalf("expected thinking field in SSE line, got %q", line)
	}
}

func TestRewriteSSELinePreservesToolCallChunks(t *testing.T) {
	t.Parallel()

	line, err := rewriteSSELine("data: {\"id\":\"chatcmpl-1\",\"model\":\"deepseek-v3.2:cloud\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Austin\\\"}\"}}]}}]}\n", "deepseek/deepseek-v3.2")
	if err != nil {
		t.Fatalf("rewriteSSELine: %v", err)
	}

	if !strings.Contains(line, "\"model\":\"deepseek/deepseek-v3.2\"") {
		t.Fatalf("expected rewritten model in SSE line, got %q", line)
	}

	if !strings.Contains(line, "\"tool_calls\"") || !strings.Contains(line, "\"get_weather\"") {
		t.Fatalf("expected tool call chunk to be preserved, got %q", line)
	}
}

func TestCopyRequestHeadersSetsBlackboxUserAgent(t *testing.T) {
	t.Parallel()

	dst := http.Header{}
	src := http.Header{}
	src.Set("User-Agent", "SomethingElse/9.9")
	src.Set("Authorization", "Bearer test")

	copyRequestHeaders(dst, src)
	dst.Set("User-Agent", blackboxUserAgent)

	if got := dst.Get("User-Agent"); got != blackboxUserAgent {
		t.Fatalf("expected user-agent %q, got %q", blackboxUserAgent, got)
	}

	if got := dst.Get("Authorization"); got != "Bearer test" {
		t.Fatalf("expected other headers to be preserved, got %q", got)
	}
}

func TestDoUpstreamRequestRetriesAnotherServerOnFailure(t *testing.T) {
	t.Parallel()

	var calls []string
	repo := &fakeRepository{}
	service := NewService(repo, models.StaticModelMapper{})
	service.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.URL.String())
			if strings.Contains(req.URL.Host, "server-a") {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":"bad gateway"}`)),
					Request:    req,
				}, nil
			}

			if got := req.Header.Get("User-Agent"); got != blackboxUserAgent {
				t.Fatalf("expected user-agent %q, got %q", blackboxUserAgent, got)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"ok"}`)),
				Request:    req,
			}, nil
		}),
	}

	resp, serverURL, err := service.doUpstreamRequest(context.Background(), []string{"http://server-a", "http://server-b"}, http.Header{}, []byte(`{"model":"alpha:cloud"}`), []byte(`{"model":"alpha:cloud"}`), "req-1", "alpha:cloud", "alpha:cloud", false)
	if err != nil {
		t.Fatalf("doUpstreamRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d", len(calls))
	}

	if serverURL != "http://server-b" {
		t.Fatalf("expected second server to succeed, got %q", serverURL)
	}

	if len(repo.logs) != 1 {
		t.Fatalf("expected one failed retry log, got %d", len(repo.logs))
	}

	if repo.logs[0].ServerURL != "http://server-a" || repo.logs[0].Success {
		t.Fatalf("expected failed log for server-a, got %#v", repo.logs[0])
	}

	if repo.logs[0].RequestJSON != `{"model":"alpha:cloud"}` {
		t.Fatalf("expected original request JSON in log, got %q", repo.logs[0].RequestJSON)
	}
}

func TestDoUpstreamRequestRetriesAfterTransportError(t *testing.T) {
	t.Parallel()

	attempts := 0
	repo := &fakeRepository{}
	service := NewService(repo, models.StaticModelMapper{})
	service.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, context.DeadlineExceeded
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"ok"}`)),
				Request:    req,
			}, nil
		}),
	}

	resp, _, err := service.doUpstreamRequest(context.Background(), []string{"http://server-a", "http://server-b"}, http.Header{}, []byte(`{"model":"alpha:cloud"}`), []byte(`{"model":"alpha:cloud"}`), "req-1", "alpha:cloud", "alpha:cloud", false)
	if err != nil {
		t.Fatalf("doUpstreamRequest: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}

	if len(repo.logs) != 1 || !strings.Contains(repo.logs[0].ErrorText, "deadline") {
		t.Fatalf("expected transport failure log, got %#v", repo.logs)
	}
}

func TestHandleCompletionsLogsSuccessfulRequest(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{servers: []string{"http://server-a"}}
	service := NewService(repo, models.StaticModelMapper{})
	service.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-1","model":"alpha:cloud","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
				Request:    req,
			}, nil
		}),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alpha:cloud","messages":[]}`))
	rec := httptest.NewRecorder()

	service.HandleCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if len(repo.logs) != 1 {
		t.Fatalf("expected one success log, got %d", len(repo.logs))
	}

	if !repo.logs[0].Success || repo.logs[0].ServerURL != "http://server-a" {
		t.Fatalf("expected successful log for server-a, got %#v", repo.logs[0])
	}

	if !strings.Contains(repo.logs[0].ResponseBody, `"content":"ok"`) {
		t.Fatalf("expected response body in log, got %q", repo.logs[0].ResponseBody)
	}
}

type fakeRepository struct {
	mu      sync.Mutex
	servers []string
	logs    []LogEntry
	err     error
}

func (f *fakeRepository) ListCandidateServers(context.Context, string) ([]string, error) {
	return f.servers, f.err
}

func (f *fakeRepository) InsertLog(_ context.Context, entry LogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, entry)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
