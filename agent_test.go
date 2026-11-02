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

func TestAgentRoundLimit(t *testing.T) {
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
	t.Setenv("REVIEW_API_KEY", "mock")
	err := runAgent("check")
	if err == nil || !strings.Contains(err.Error(), "8轮") || count != 8 {
		t.Fatalf("expected limit, got %v and %d calls", err, count)
	}
}
