package tools

import (
	"fmt"
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
	result, err := Execute("read_file", Args{Path: "ok.go", Start: 1, End: 999}, []string{"ok.go"})
	if err != nil || !strings.Contains(result, "读取范围 1-1") || !strings.Contains(result, "next_start=0") {
		t.Fatal("read beyond EOF should return only available lines", result, err)
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

func TestReadFileInclusiveLineLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	lines := []string{"package demo"}
	for n := 2; n <= 280; n++ {
		lines = append(lines, fmt.Sprintf("// marker%d", n))
	}
	writeCode(t, "demo.go", strings.Join(lines, "\n"))
	files := []string{"demo.go"}
	for _, span := range []struct {
		start, end, actualEnd, next int
		truncated                   bool
	}{
		{1, 120, 120, 121, false},
		{121, 240, 240, 241, false},
		{120, 239, 239, 240, false},
		{120, 240, 239, 240, true},
		{120, 280, 239, 240, true},
		{240, 999, 280, 0, false},
		{1, int(^uint(0) >> 1), 120, 121, true},
	} {
		result, err := Execute("read_file", Args{Path: "demo.go", Start: span.start, End: span.end}, files)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, fmt.Sprintf("读取范围 %d-%d\n", span.start, span.actualEnd)) ||
			!strings.Contains(result, fmt.Sprintf("truncated=%t next_start=%d", span.truncated, span.next)) {
			t.Fatalf("incorrect page metadata: %s", result)
		}
		sourceLines := strings.Split(strings.TrimSpace(result), "\n")[2:]
		if len(sourceLines) != span.actualEnd-span.start+1 || len(sourceLines) > 120 {
			t.Fatalf("unexpected number of source lines: %d", len(sourceLines))
		}
		for i, sourceLine := range sourceLines {
			n := span.start + i
			if sourceLine != fmt.Sprintf("%d %s", n, lines[n-1]) {
				t.Fatalf("wrong source line %d: %q", n, sourceLine)
			}
		}
	}
	for _, span := range []struct{ start, end int }{{281, 999}, {120, 119}, {1, -1}} {
		if _, err := Execute("read_file", Args{Path: "demo.go", Start: span.start, End: span.end}, files); err == nil {
			t.Fatalf("invalid range accepted: %+v", span)
		}
	}
}
