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

func TestAgentToolLoop(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("example.go", []byte("package demo\nfunc Broken() int { return -1 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := git("init"); err != nil {
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
	if err := runAgent("check code"); err != nil {
		t.Fatal(err)
	}
	if round != 2 {
		t.Fatalf("got %d requests", round)
	}
}

func TestAgentReadScope(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("ok.go", []byte("package demo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentRead("ok.go", []string{"ok.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agentRead("../secret.go", []string{"ok.go"}); err == nil {
		t.Fatal("unlisted path allowed")
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentRead(outside, []string{outside}); err == nil {
		t.Fatal("outside path allowed")
	}
	if _, err := agentTool("read_file", agentArgs{Path: "ok.go", Start: 1, End: 999}, []string{"ok.go"}); err == nil {
		t.Fatal("large read allowed")
	}
}

func TestAgentRejectsToolsDuringSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := git("init"); err != nil {
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
	err := runAgent("check")
	if err == nil || !strings.Contains(err.Error(), "汇报轮") || count != 4 {
		t.Fatalf("expected limit, got %v and %d calls", err, count)
	}
}

func TestAgentReservedSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := git("init"); err != nil {
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
	if err := runAgent("check"); err != nil {
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
	if _, err := git("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := git("add", "--", "target.go", "other.go"); err != nil {
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
	if err := runFileAgent("check", "target.go"); err != nil {
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
	if err := runFileAgent("check", root); err != nil {
		t.Fatal(err)
	}
	current, err := os.Getwd()
	if err != nil || current != previous {
		t.Fatal("working directory not restored")
	}
}
