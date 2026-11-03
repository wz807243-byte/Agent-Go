package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/Agent-Go/internal/code"
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

func TestAgentReadScope(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("ok.go", []byte("package demo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read("ok.go", []string{"ok.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Read("../secret.go", []string{"ok.go"}); err == nil {
		t.Fatal("unlisted path allowed")
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(outside, []string{outside}); err == nil {
		t.Fatal("outside path allowed")
	}
	if _, err := Execute("read_file", Args{Path: "ok.go", Start: 1, End: 999}, []string{"ok.go"}); err == nil {
		t.Fatal("large read allowed")
	}
}

func TestGitReadOutputLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	code.Git("init")
	writeCode(t, "large.go", strings.Repeat("x", 256001))
	code.Git("add", "large.go")
	if _, err := versionRead("large.go", "staged", []string{"large.go"}); err == nil || !strings.Contains(err.Error(), "256 KB") {
		t.Fatal("unbounded git output", err)
	}
}
