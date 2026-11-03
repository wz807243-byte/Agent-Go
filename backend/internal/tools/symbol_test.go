package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"example.com/Agent-Go/internal/code"
)

func TestVersionedSymbols(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := code.Git("init"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int { return 1 }\n")
	if _, err := code.Git("add", "demo.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := code.Git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeCode(t, "demo.go", "package demo\nfunc Value() int { return 2 }\n")
	code.Git("add", "demo.go")
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
		result, err := Execute("list_symbols", Args{Path: "demo.go", Version: v}, files)
		if err != nil {
			t.Fatal(err)
		}
		var page struct{ Symbols []code.SymbolEntry }
		if err = json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Symbols) != 1 {
			t.Fatal(result)
		}
		code, err := Execute("read_symbol", Args{SymbolID: page.Symbols[0].ID}, files)
		if err != nil || !strings.Contains(code, fmt.Sprintf("return %d", i+1)) {
			t.Fatal(code, err)
		}
		if v == "working" {
			writeCode(t, "demo.go", src+"\n")
			if _, err := Execute("read_symbol", Args{SymbolID: page.Symbols[0].ID}, files); err == nil {
				t.Fatal("stale symbol accepted")
			}
		}
	}
	if _, err := versionRead("../demo.go", "head", []string{"../demo.go"}); err == nil {
		t.Fatal("escaped snapshot")
	}
	if _, err := versionRead("demo.go", "other", files); err == nil {
		t.Fatal("unknown version")
	}
	code.Git("rm", "-f", "demo.go")
	listed, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0] != "demo.go" {
		t.Fatal("deleted HEAD file inaccessible", listed)
	}
	for _, v := range []string{"head", "staged", "working"} {
		result, err := pageTool("list_files", Args{Version: v}, listed)
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

func TestSymbolReceiversAndLongDeclarations(t *testing.T) {
	t.Chdir(t.TempDir())
	src := "package demo\ntype A struct{}\ntype B struct{}\nfunc (a *A) Run() {}\nfunc (b B) Run() {}\nfunc Long() {\n" + strings.Repeat("// some comment\n", 1000) + "}\n"
	writeCode(t, "demo.go", src)
	entries, err := code.FileSymbols("demo.go", "working", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 || entries[2].Name != "*A.Run" || entries[3].Name != "B.Run" || entries[2].ID == entries[3].ID {
		t.Fatal(entries)
	}
	a := Args{SymbolID: entries[4].ID}
	if _, err := symbolTool("read_symbol", a, []string{"demo.go"}); err == nil {
		t.Fatal("oversized symbol accepted")
	}
	a.Start = entries[4].First
	a.End = a.Start + 5
	if _, err := symbolTool("read_symbol", a, []string{"demo.go"}); err != nil {
		t.Fatal(err)
	}
}
