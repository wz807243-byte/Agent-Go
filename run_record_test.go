package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			err := runFileAgent("check", root)
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
			var record runRecord
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
