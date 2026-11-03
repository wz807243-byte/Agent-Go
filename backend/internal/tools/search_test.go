package tools

import (
	"encoding/json"
	"fmt"
	"testing"
)

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
		result, err := Execute("search_code", Args{Query: "needle", PathPrefix: "pkg", Cursor: cursor, Limit: 30}, files)
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
	result, err := Execute("list_files", Args{Cursor: 200, Limit: 10}, files)
	if err != nil {
		t.Fatal(err)
	}
	var page codePage
	json.Unmarshal([]byte(result), &page)
	if len(page.Items) != 5 || page.Truncated {
		t.Fatal(result)
	}
	result, err = Execute("search_code", Args{Query: "needle", PathPrefix: "missing"}, files)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(result), &page)
	if len(page.Items) != 0 {
		t.Fatal(result)
	}
}
