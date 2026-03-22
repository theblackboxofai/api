package models

import (
	"encoding/json"
	"strings"
	"time"
)

type usagePayload struct {
	Usage *tokenUsage `json:"usage"`
}

type tokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

func BuildRequestHistory(records []RequestHistoryRecord, now time.Time) RequestHistorySection {
	last24Cutoff := now.Add(-24 * time.Hour)
	last7Cutoff := now.Add(-7 * 24 * time.Hour)
	last28Cutoff := now.Add(-28 * 24 * time.Hour)

	last24Requests := make(map[string]struct{})
	last7Requests := make(map[string]struct{})
	last28Requests := make(map[string]struct{})

	var history RequestHistorySection
	for _, record := range records {
		if record.CreatedAt.Before(last28Cutoff) {
			continue
		}

		usage := extractUsage(record.ResponseBody)

		accumulateRequestHistory(&history.Last28Days, last28Requests, record, usage)
		if !record.CreatedAt.Before(last7Cutoff) {
			accumulateRequestHistory(&history.Last7Days, last7Requests, record, usage)
		}
		if !record.CreatedAt.Before(last24Cutoff) {
			accumulateRequestHistory(&history.Last24Hours, last24Requests, record, usage)
		}
	}

	return history
}

func accumulateRequestHistory(window *RequestHistoryWindow, requests map[string]struct{}, record RequestHistoryRecord, usage tokenUsage) {
	window.AttemptCount++
	if record.Success {
		window.SuccessCount++
	} else {
		window.FailureCount++
	}

	if record.RequestID != "" {
		if _, ok := requests[record.RequestID]; !ok {
			requests[record.RequestID] = struct{}{}
			window.RequestCount++
		}
	}

	window.PromptTokens += usage.PromptTokens
	window.CompletionTokens += usage.CompletionTokens
	window.TotalTokens += usage.TotalTokens
}

func extractUsage(body string) tokenUsage {
	if usage, ok := parseUsageJSON([]byte(body)); ok {
		return usage
	}

	var lastUsage tokenUsage
	var found bool
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		if usage, ok := parseUsageJSON([]byte(payload)); ok {
			lastUsage = usage
			found = true
		}
	}

	if found {
		return lastUsage
	}

	return tokenUsage{}
}

func parseUsageJSON(body []byte) (tokenUsage, bool) {
	var payload usagePayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Usage == nil {
		return tokenUsage{}, false
	}

	usage := *payload.Usage
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return usage, true
}
