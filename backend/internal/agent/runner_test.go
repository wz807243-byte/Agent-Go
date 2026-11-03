package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/Agent-Go/internal/code"
	taskcontext "example.com/Agent-Go/internal/context"
	"example.com/Agent-Go/internal/observability"
)

func TestAgentToolLoop(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("example.go", []byte("package demo\nfunc Broken() int { return -1 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := code.Git("init"); err != nil {
		t.Fatal(err)
	}
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var body struct{ Messages []json.RawMessage }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if round == 1 {
			fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"look at code","tool_calls":[{"id":"read1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"example.go\",\"start\":1,\"end\":2}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
		} else {
			if len(body.Messages) != 4 || !strings.Contains(string(body.Messages[3]), "Broken") || !strings.Contains(string(body.Messages[2]), "reasoning_content") {
				t.Error("tool result or assistant metadata missing")
			}
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"example.go:2 returns negative"}}],"usage":{"prompt_tokens":20,"completion_tokens":6}}`)
		}
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	t.Setenv("REVIEW_API_KEY", "mock")
	if err := Run("check code"); err != nil {
		t.Fatal(err)
	}
	if round != 2 {
		t.Fatalf("got %d requests", round)
	}
}

func TestAgentRejectsToolsDuringSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := code.Git("init"); err != nil {
		t.Fatal(err)
	}
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"list_files","arguments":"{}"}}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	t.Setenv("REVIEW_API_KEY", "mock")
	err := Run("check")
	if err == nil || !strings.Contains(err.Error(), "汇报轮") || count != 4 {
		t.Fatalf("expected limit, got %v and %d calls", err, count)
	}
}

func TestAgentReservedSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := code.Git("init"); err != nil {
		t.Fatal(err)
	}
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if count == 8 {
			if body["tool_choice"] != "none" {
				t.Error("last round must reserve a report")
			}
			if body["max_tokens"] != float64(taskcontext.ReportCompletionTokens) {
				t.Error("report must have its own output budget")
			}
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Partial review, no confirmed findings"}}]}`)
			return
		}
		args, _ := json.Marshal(map[string]string{"query": fmt.Sprintf("query%d", count)})
		message := map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": fmt.Sprint(count), "type": "function", "function": map[string]any{"name": "search_code", "arguments": string(args)}}}}}}}
		json.NewEncoder(w).Encode(message)
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	t.Setenv("REVIEW_API_KEY", "mock")
	if err := Run("check"); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("expected 8 calls, got %d", count)
	}
}

func TestFileAgentRestrictsAllTools(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, src := range map[string]string{
		"target.go": "package demo\nfunc Target() int {return 1}\n",
		"other.go":  "package demo\nfunc PRIVATE_MARKER() {}\n",
	} {
		if err := os.WriteFile(name, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := code.Git("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := code.Git("add", "--", "target.go", "other.go"); err != nil {
		t.Fatal(err)
	}
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var body struct{ Messages []json.RawMessage }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if round == 1 {
			calls := []any{}
			for i, spec := range []struct{ name, args string }{
				{"list_files", "{}"},
				{"search_code", `{"query":"func"}`},
				{"read_file", `{"path":"other.go"}`},
				{"staged_diff", `{"path":"other.go"}`},
			} {
				calls = append(calls, map[string]any{"id": fmt.Sprint(i), "type": "function", "function": map[string]string{"name": spec.name, "arguments": spec.args}})
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": calls}}}})
		} else {
			if len(body.Messages) != 7 {
				t.Errorf("expected 4 tool results")
			}
			for _, m := range body.Messages[3:] {
				if strings.Contains(string(m), "PRIVATE_MARKER") {
					t.Error("other file content leaked")
				}
			}
			if !strings.Contains(string(body.Messages[5]), "工具错误") {
				t.Error("out-of-scope read not rejected")
			}
			if !strings.Contains(string(body.Messages[6]), "指定路径模式") {
				t.Error("scoped diff explanation missing")
			}
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"target file reviewed"}}]}`)
		}
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	t.Setenv("REVIEW_API_KEY", "mock")
	if err := RunPath("check", "target.go"); err != nil {
		t.Fatal(err)
	}
	if round != 2 {
		t.Fatalf("expected 2 requests, got %d", round)
	}
}

func TestExternalDirectoryAgent(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "external demo")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte("package demo\nfunc Demo() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "private.go"), []byte("PRIVATE_MARKER"), 0600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var body struct{ Messages []json.RawMessage }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if round == 1 {
			fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"demo.go\"}"}},{"id":"2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"../private.go\"}"}}]}}]}`)
		} else {
			if !strings.Contains(string(body.Messages[3]), "Demo") {
				t.Error("external file was not read")
			}
			if !strings.Contains(string(body.Messages[4]), "工具错误") || strings.Contains(string(body.Messages[4]), "PRIVATE_MARKER") {
				t.Error("scope escaped")
			}
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"external folder inspected"}}]}`)
		}
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	t.Setenv("REVIEW_API_KEY", "mock")
	if err := RunPath("check", root); err != nil {
		t.Fatal(err)
	}
	current, err := os.Getwd()
	if err != nil || current != previous {
		t.Fatal("working directory not restored")
	}
}

func writeCode(t *testing.T, p, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateAndFailedToolsDoNotEndImmediately(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			t.Chdir(t.TempDir())
			code.Git("init")
			writeCode(t, "demo.go", "package demo\n")
			count := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				var request map[string]json.RawMessage
				json.NewDecoder(r.Body).Decode(&request)
				if count == 3 && string(request["tool_choice"]) == `"none"` {
					t.Error("one duplicate/error prematurely ended exploration")
				}
				if count <= 3 {
					name, args := "list_files", "{}"
					if failed {
						if count == 2 {
							writeCode(t, "demo.go", "package demo\n"+strings.Repeat("// extended\n", 100))
						}
						name, args = "read_file", `{"path":"demo.go","start":99}`
					}
					if count == 3 {
						name, args = "search_code", `{"query":"package"}`
					}
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": fmt.Sprint(count), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}}}})
				} else {
					fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
				}
			}))
			defer srv.Close()
			t.Setenv("REVIEW_URL", srv.URL)
			t.Setenv("REVIEW_API_KEY", "mock")
			logs := t.TempDir()
			t.Setenv("AGENT_LOG_DIR", logs)
			if err := Run("review"); err != nil {
				t.Fatal(err)
			}
			paths, _ := filepath.Glob(filepath.Join(logs, "*.json"))
			data, _ := os.ReadFile(paths[0])
			var record observability.RunRecord
			json.Unmarshal(data, &record)
			if len(record.Tools) != 3 || record.Tools[1].Cached == failed {
				t.Fatalf("wrong cache record: %+v", record.Tools)
			}
		})
	}
}

func TestDefaultVersionFollowsStagedDiff(t *testing.T) {
	t.Chdir(t.TempDir())
	code.Git("init")
	writeCode(t, "demo.go", "package demo\nfunc Value() int {return 1}\n")
	code.Git("add", "demo.go")
	if _, err := code.Git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int {return 2} // STAGED_MARKER\n")
	code.Git("add", "demo.go")
	writeCode(t, "demo.go", "package demo\nfunc Value() int {return 3} // WORKING_MARKER\n")
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var body struct{ Messages []json.RawMessage }
		json.NewDecoder(r.Body).Decode(&body)
		if round == 3 {
			last := string(body.Messages[len(body.Messages)-1])
			if !strings.Contains(last, "STAGED_MARKER") || strings.Contains(last, "WORKING_MARKER") {
				t.Error("mixed versions", last)
			}
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
			return
		}
		name, args := "staged_diff", "{}"
		if round == 2 {
			name, args = "read_file", `{"path":"demo.go"}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": fmt.Sprint(round), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}}}})
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_API_KEY", "mock")
	t.Setenv("AGENT_LOG_DIR", t.TempDir())
	if err := Run("审查暂存变更"); err != nil {
		t.Fatal(err)
	}
	if round != 3 {
		t.Fatal(round)
	}
}

func TestRunRecordsAndInstructions(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			root := t.TempDir()
			logs := t.TempDir()
			os.WriteFile(filepath.Join(root, "demo.go"), []byte("package demo"), 0600)
			os.WriteFile(filepath.Join(root, "任务说明.md"), []byte("BUSINESS_REQUIREMENT"), 0600)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct{ Messages []json.RawMessage }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				joined := fmt.Sprint(request.Messages)
				raw, _ := json.Marshal(request)
				if !strings.Contains(string(raw), "BUSINESS_REQUIREMENT") {
					t.Errorf("missing task instructions: %s", joined)
				}
				if failed {
					w.WriteHeader(401)
					return
				}
				fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"finished"}}],"usage":{"prompt_tokens":40,"completion_tokens":5,"prompt_cache_hit_tokens":12,"completion_tokens_details":{"reasoning_tokens":3}}}`)
			}))
			defer srv.Close()
			t.Setenv("REVIEW_URL", srv.URL)
			t.Setenv("REVIEW_MODEL", "mock")
			t.Setenv("REVIEW_API_KEY", "mock-secret")
			t.Setenv("AGENT_LOG_DIR", logs)
			err := RunPath("check", root)
			if (err != nil) != failed {
				t.Fatalf("error=%v", err)
			}
			paths, _ := filepath.Glob(filepath.Join(logs, "*.json"))
			if len(paths) != 1 {
				t.Fatalf("records=%v", paths)
			}
			data, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "mock-secret") {
				t.Fatal("key logged")
			}
			var record observability.RunRecord
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if len(record.Instructions) != 1 {
				t.Fatal("loaded instruction paths not saved")
			}
			if failed {
				if record.Status != "failed" || record.Error == "" || record.UsageComplete {
					t.Fatal("failed task status wrong")
				}
			} else {
				if record.Status != "reported" || !record.UsageComplete || record.Total != 45 || record.Input != 40 || record.Output != 5 {
					t.Fatal("usage totals wrong")
				}
				if !strings.Contains(string(record.Rounds[0]), "reasoning_tokens") || !strings.Contains(string(record.Rounds[0]), "prompt_cache_hit_tokens") {
					t.Fatal("usage details lost")
				}
			}
		})
	}
}

// Replay truncation both before reporting and at the last reserved report round.
// An interrupted tool call must never be executed or sent back as an assistant
// message without matching tool results.
func TestLengthRecovery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		truncateAt int
		retry      string
	}{
		{"early", 1, "stop"},
		{"last_round", 8, "stop"},
		{"retry_truncated", 8, "length"},
		{"retry_http_failure", 8, "http"},
		{"reasoning_only", 1, "empty_partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, logs := t.TempDir(), t.TempDir()
			writeCode(t, filepath.Join(root, "demo.go"), "package demo\nfunc Demo() {}\n")
			count, expectedInput, expectedOutput := 0, 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				var request struct {
					Messages   []json.RawMessage `json:"messages"`
					MaxTokens  int               `json:"max_tokens"`
					ToolChoice string            `json:"tool_choice"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if count > tc.truncateAt+1 {
					t.Error("unbounded report retries")
					w.WriteHeader(500)
					return
				}
				wantTokens := taskcontext.MaxCompletionTokens
				if count == taskcontext.MaxRounds {
					wantTokens = taskcontext.ReportCompletionTokens
				}
				if count > tc.truncateAt {
					wantTokens = taskcontext.ReportCompletionTokens
					if tc.truncateAt == taskcontext.MaxRounds {
						wantTokens = taskcontext.ReportRetryCompletionTokens
					}
					if request.ToolChoice != "none" {
						t.Error("length recovery must disable tools")
					}
					for _, message := range request.Messages {
						if strings.Contains(string(message), "INTERRUPTED_TOOL") {
							t.Error("truncated assistant message was replayed")
						}
					}
					if tc.retry == "http" {
						w.WriteHeader(503)
						return
					}
				}
				if request.MaxTokens != wantTokens {
					t.Errorf("round %d: max_tokens=%d, want %d", count, request.MaxTokens, wantTokens)
				}
				finish, output := "stop", 20
				message := map[string]any{"role": "assistant", "content": "COMPLETE_REPORT"}
				if count < tc.truncateAt {
					finish = "tool_calls"
					args, _ := json.Marshal(map[string]string{"query": fmt.Sprintf("query%d", count)})
					message = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": fmt.Sprint(count), "type": "function", "function": map[string]string{"name": "search_code", "arguments": string(args)}}}}
				} else if count == tc.truncateAt || tc.retry == "length" {
					finish, output = "length", request.MaxTokens
					message = map[string]any{"role": "assistant", "content": "PARTIAL_REPORT", "tool_calls": []any{map[string]any{"id": "INTERRUPTED_TOOL", "type": "function", "function": map[string]string{"name": "read_file", "arguments": `{"path":`}}}}
					if tc.retry == "empty_partial" {
						message = map[string]any{"role": "assistant", "content": nil, "reasoning_content": "INTERNAL_REASONING"}
					}
				}
				expectedInput += 10
				expectedOutput += output
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": message}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": output}})
			}))
			defer srv.Close()
			t.Setenv("REVIEW_URL", srv.URL)
			t.Setenv("REVIEW_MODEL", "mock")
			t.Setenv("REVIEW_API_KEY", "mock")
			t.Setenv("AGENT_LOG_DIR", logs)
			err := RunPath("check", root)
			wantFailure := tc.retry == "length" || tc.retry == "http"
			if (err != nil) != wantFailure {
				t.Fatalf("error=%v, wantFailure=%v", err, wantFailure)
			}
			if tc.retry == "length" && !strings.Contains(err.Error(), "长度上限") {
				t.Fatal(err)
			}
			if count != tc.truncateAt+1 {
				t.Fatalf("got %d requests, want %d", count, tc.truncateAt+1)
			}
			paths, err := filepath.Glob(filepath.Join(logs, "*.json"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("log paths=%v, err=%v", paths, err)
			}
			data, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			var record observability.RunRecord
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if record.ReportRetries != 1 || record.Input != expectedInput || record.Output != expectedOutput || len(record.Rounds) != expectedInput/10 {
				t.Fatalf("retry accounting wrong: %+v", record)
			}
			wantPartials := 1
			if tc.retry == "length" {
				wantPartials = 2
			} else if tc.retry == "empty_partial" {
				wantPartials = 0
			}
			if len(record.PartialReports) != wantPartials {
				t.Fatalf("partial reports lost: %+v", record.PartialReports)
			}
			for _, partial := range record.PartialReports {
				if partial != "PARTIAL_REPORT" {
					t.Fatalf("unexpected partial report: %q", partial)
				}
			}
			if wantFailure {
				if record.Status != "failed" || record.Report != "" || record.Error == "" {
					t.Fatal("incomplete report marked complete", record)
				}
			} else if record.Status != "reported" || record.Report != "COMPLETE_REPORT" || !record.UsageComplete {
				t.Fatal("completed retry not recorded", record)
			}
		})
	}
}

func TestOversizedReadsDoNotForceEarlyReport(t *testing.T) {
	root, logs := t.TempDir(), t.TempDir()
	lines := []string{"package demo"}
	for n := 2; n <= 280; n++ {
		lines = append(lines, fmt.Sprintf("// marker%d", n))
	}
	writeCode(t, filepath.Join(root, "main.go"), strings.Join(lines, "\n"))
	spans := []struct{ start, end int }{{1, 120}, {120, 280}, {120, 240}, {240, 280}}
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		var request struct {
			Messages   []json.RawMessage `json:"messages"`
			ToolChoice string            `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if request.ToolChoice == "none" {
			t.Error("oversized reads caused premature reporting")
		}
		if count == 3 || count == 4 {
			last := string(request.Messages[len(request.Messages)-1])
			if !strings.Contains(last, "truncated=true next_start=240") || strings.Contains(last, "工具错误") {
				t.Error("read should return a bounded page and its continuation", last)
			}
		}
		if count <= len(spans) {
			span := spans[count-1]
			args, _ := json.Marshal(map[string]any{"path": "main.go", "start": span.start, "end": span.end})
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": fmt.Sprint(count), "type": "function", "function": map[string]string{"name": "read_file", "arguments": string(args)}}}}}}})
			return
		}
		last := string(request.Messages[len(request.Messages)-1])
		if !strings.Contains(last, "280 // marker280") || !strings.Contains(last, "next_start=0") {
			t.Error("file tail was not read", last)
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"file tail checked"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("REVIEW_URL", srv.URL)
	t.Setenv("REVIEW_MODEL", "mock")
	t.Setenv("REVIEW_API_KEY", "mock")
	t.Setenv("AGENT_LOG_DIR", logs)
	if err := RunPath("check", root); err != nil {
		t.Fatal(err)
	}
	if count != len(spans)+1 {
		t.Fatalf("got %d requests, want %d", count, len(spans)+1)
	}
	paths, err := filepath.Glob(filepath.Join(logs, "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("log paths=%v, err=%v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var record observability.RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != "reported" || len(record.Tools) != len(spans) {
		t.Fatal("read sequence not completed", record)
	}
	for _, call := range record.Tools {
		if call.Failed {
			t.Fatal("oversized read marked failed", call)
		}
	}
}
