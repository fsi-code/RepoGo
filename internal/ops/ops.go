package ops

import (
	"encoding/json"
	"time"
)

type Response struct {
	Clipdev    string          `json:"_clipdev"`
	ID         string          `json:"id"`
	OK         bool            `json:"ok"`
	Op         string          `json:"op"`
	Result     json.RawMessage `json:"result,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	DurationMs int64           `json:"duration_ms"`
	Error      string          `json:"error,omitempty"`
	Code       string          `json:"code,omitempty"`
	DryRun     bool            `json:"dry_run,omitempty"`
}

func Success(id, op, result string, dur time.Duration, truncated bool) *Response {
	b, _ := json.Marshal(result)
	return &Response{
		Clipdev:    "1.0",
		ID:         id,
		OK:         true,
		Op:         op,
		Result:     json.RawMessage(b),
		Truncated:  truncated,
		DurationMs: dur.Milliseconds(),
	}
}

func Failure(id, op, msg, code string) *Response {
	return &Response{
		Clipdev: "1.0",
		ID:      id,
		OK:      false,
		Op:      op,
		Error:   msg,
		Code:    code,
	}
}

func DryRunResult(id, op, preview string) *Response {
	b, _ := json.Marshal(preview)
	return &Response{
		Clipdev:    "1.0",
		ID:         id,
		OK:         true,
		Op:         op,
		Result:     json.RawMessage(b),
		DurationMs: 0,
		DryRun:     true,
	}
}

func truncate(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	return s[:max] + "\n... [truncated]", true
}
