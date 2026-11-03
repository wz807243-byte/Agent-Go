package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAstGrepIntegration(t *testing.T) {
	bin := os.Getenv("TEST_AST_GREP_BIN")
	if bin == "" {
		t.Skip("set TEST_AST_GREP_BIN for installed ast-grep integration")
	}
	t.Setenv("AGENT_AST_GREP", "1")
	t.Setenv("AST_GREP_BIN", bin)
	t.Chdir(t.TempDir())
	writeCode(t, "demo.go", "package demo\nfunc Demo(){ foo(1); foo(2) }\n")
	writeCode(t, "private.go", "package demo\nfunc Secret(){ foo(99) }\n")
	result, err := structureTool(Args{Pattern: "foo($A)", Limit: 1}, []string{"demo.go"})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Matches    []struct{ Text string }
		Truncated  bool
		NextCursor int `json:"next_cursor"`
	}
	if err = json.Unmarshal([]byte(result), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Matches) != 1 || !page.Truncated || page.NextCursor != 1 || strings.Contains(result, "99") {
		t.Fatal(result)
	}
	result, err = structureTool(Args{Pattern: "foo($A)", Limit: 1, Cursor: 1}, []string{"demo.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "foo(2)") {
		t.Fatal(result)
	}
	result, err = structureTool(Args{Pattern: "absent()"}, []string{"demo.go"})
	if err != nil || !strings.Contains(result, `"matches":[]`) {
		t.Fatal("no-match", result, err)
	}
	if _, err = structureTool(Args{Pattern: "foo($A)", Path: "private.go"}, []string{"demo.go"}); err == nil {
		t.Fatal("out-of-scope structure read")
	}
	if _, err = structureTool(Args{Pattern: "foo($A)", Path: "demo.go", PathPrefix: "other"}, []string{"demo.go"}); err == nil {
		t.Fatal("path escaped path_prefix")
	}
	t.Setenv("AST_GREP_BIN", filepath.Join(t.TempDir(), "missing-ast-grep.exe"))
	if _, err = structureTool(Args{Pattern: "foo($A)"}, []string{"demo.go"}); err == nil {
		t.Fatal("missing executable accepted")
	}
}
