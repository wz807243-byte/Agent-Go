package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFeatureGates(t *testing.T) {
	t.Setenv("AGENT_AST_GREP", "")
	t.Setenv("AGENT_SYMBOLS", "0")
	data, _ := json.Marshal(Definitions())
	if strings.Contains(string(data), "search_structure") || strings.Contains(string(data), "list_symbols") {
		t.Fatal("disabled tools exposed")
	}
	if _, err := Execute("search_structure", Args{Pattern: "foo()"}, nil); err == nil {
		t.Fatal("disabled tool executed")
	}
	t.Setenv("AGENT_AST_GREP", "1")
	t.Setenv("AGENT_SYMBOLS", "1")
	data, _ = json.Marshal(Definitions())
	if !strings.Contains(string(data), "search_structure") || !strings.Contains(string(data), "list_symbols") {
		t.Fatal("enabled tools missing")
	}
}
