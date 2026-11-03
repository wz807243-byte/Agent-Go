package observability

import (
	"encoding/json"
	"time"

	"example.com/Agent-Go/internal/tools"
)

type ToolRecord struct {
	Name      string     `json:"name"`
	Arguments tools.Args `json:"arguments"`
	Cached    bool       `json:"cached"`
	Bytes     int        `json:"result_bytes"`
	Failed    bool       `json:"failed"`
}

type RunRecord struct {
	Features       map[string]bool   `json:"features"`
	Tools          []ToolRecord      `json:"tools"`
	Started        time.Time         `json:"started_at"`
	DurationMS     int64             `json:"duration_ms"`
	Project        string            `json:"project"`
	Task           string            `json:"task"`
	Model          string            `json:"configured_model"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
	Report         string            `json:"report,omitempty"`
	PartialReports []string          `json:"partial_reports,omitempty"`
	ReportRetries  int               `json:"report_retries,omitempty"`
	Instructions   []string          `json:"instructions_loaded"`
	Input          int               `json:"input_tokens_including_cache"`
	Output         int               `json:"output_tokens"`
	Total          int               `json:"total_tokens"`
	UsageComplete  bool              `json:"usage_complete"`
	Rounds         []json.RawMessage `json:"round_usage"`
}
