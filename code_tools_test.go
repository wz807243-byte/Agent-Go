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

func writeCode(t *testing.T, p, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPastOriginalLimits(t *testing.T) {
	t.Chdir(t.TempDir())
	files := []string{}
	for i := 0; i < 205; i++ {
		p := fmt.Sprintf("pkg/f%03d.go", i)
		writeCode(t, p, "package demo\n// needle\n")
		files = append(files, p)
	}
	cursor, total := 0, 0
	for {
		result, err := agentTool("search_code", agentArgs{Query: "needle", PathPrefix: "pkg", Cursor: cursor, Limit: 30}, files)
		if err != nil {
			t.Fatal(err)
		}
		var page codePage
		if err = json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatal(err)
		}
		total += len(page.Items)
		if !page.Truncated {
			break
		}
		if page.NextCursor <= cursor {
			t.Fatal("pagination stalled")
		}
		cursor = page.NextCursor
	}
	if total != 205 {
		t.Fatalf("searched %d matches", total)
	}
	result, err := agentTool("list_files", agentArgs{Cursor: 200, Limit: 10}, files)
	if err != nil {
		t.Fatal(err)
	}
	var page codePage
	json.Unmarshal([]byte(result), &page)
	if len(page.Items) != 5 || page.Truncated {
		t.Fatal(result)
	}
	result, err = agentTool("search_code", agentArgs{Query: "needle", PathPrefix: "missing"}, files)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(result), &page)
	if len(page.Items) != 0 {
		t.Fatal(result)
	}
}

func TestVersionedCode(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := git("init"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int { return 1 }\n")
	if _, err := git("add", "demo.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int { return 2 }\n")
	git("add", "demo.go")
	writeCode(t, "demo.go", "package demo\nfunc Value() int { return 3 }\n")
	files := []string{"demo.go"}
	for i, v := range []string{"head", "staged", "working"} {
		src, err := versionRead("demo.go", v, files)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(src, fmt.Sprintf("return %d", i+1)) {
			t.Fatal(v, src)
		}

	}
	if _, err := versionRead("../demo.go", "head", []string{"../demo.go"}); err == nil {
		t.Fatal("escaped snapshot")
	}
	if _, err := versionRead("demo.go", "other", files); err == nil {
		t.Fatal("unknown version")
	}
	git("rm", "-f", "demo.go")
	listed, err := agentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0] != "demo.go" {
		t.Fatal("deleted HEAD file inaccessible", listed)
	}
	for _, v := range []string{"head", "staged", "working"} {
		result, err := pageTool("list_files", agentArgs{Version: v}, listed)
		if err != nil {
			t.Fatal(err)
		}
		var page codePage
		json.Unmarshal([]byte(result), &page)
		want := 0
		if v == "head" {
			want = 1
		}
		if len(page.Items) != want {
			t.Fatal("wrong version file set", v, result)
		}
	}
}

func TestDuplicateAndFailedToolsDoNotEndImmediately(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			t.Chdir(t.TempDir())
			git("init")
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
			if err := runAgent("review"); err != nil {
				t.Fatal(err)
			}
			paths, _ := filepath.Glob(filepath.Join(logs, "*.json"))
			data, _ := os.ReadFile(paths[0])
			var record runRecord
			json.Unmarshal(data, &record)
			if len(record.Tools) != 3 || record.Tools[1].Cached == failed {
				t.Fatalf("wrong cache record: %+v", record.Tools)
			}
		})
	}
}

func TestDefaultVersionFollowsStagedDiff(t *testing.T) {
	t.Chdir(t.TempDir())
	git("init")
	writeCode(t, "demo.go", "package demo\nfunc Value() int {return 1}\n")
	git("add", "demo.go")
	if _, err := git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int {return 2} // STAGED_MARKER\n")
	git("add", "demo.go")
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
	if err := runAgent("审查暂存变更"); err != nil {
		t.Fatal(err)
	}
	if round != 3 {
		t.Fatal(round)
	}
}

func TestGitReadOutputLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	git("init")
	writeCode(t, "large.go", strings.Repeat("x", 256001))
	git("add", "large.go")
	if _, err := versionRead("large.go", "staged", []string{"large.go"}); err == nil || !strings.Contains(err.Error(), "256 KB") {
		t.Fatal("unbounded git output", err)
	}
}
