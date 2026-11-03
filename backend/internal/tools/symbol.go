package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"example.com/Agent-Go/internal/code"
)

func symbolTool(name string, a Args, files []string) (string, error) {
	v, err := code.CodeVersion(a.Version)
	if err != nil {
		return "", err
	}
	var ref code.SymbolRef
	if name == "read_symbol" {
		ref, err = code.ParseSymbolID(a.SymbolID)
		if err != nil {
			return "", err
		}
		a.Path = ref.Path
		v = ref.Version
	}
	src, err := versionRead(a.Path, v, files)
	if err != nil {
		return "", err
	}
	entries, err := code.FileSymbols(a.Path, v, src)
	if err != nil {
		return "", err
	}
	if name == "list_symbols" {
		limit := a.Limit
		if limit == 0 {
			limit = 30
		}
		if a.Cursor < 0 || a.Cursor > len(entries) || limit < 1 || limit > 200 {
			return "", fmt.Errorf("无效 cursor 或 limit")
		}
		end := min(a.Cursor+limit, len(entries))
		next := 0
		if end < len(entries) {
			next = end
		}
		data, err := json.Marshal(map[string]any{"version": v, "symbols": entries[a.Cursor:end], "truncated": end < len(entries), "next_cursor": next})
		if len(data) > 24000 {
			return "", fmt.Errorf("符号列表过长，请减小 limit")
		}
		return string(data), err
	}
	for _, e := range entries {
		if e.ID == a.SymbolID {
			lines := strings.Split(src, "\n")
			start, end := e.First, e.Last
			if a.Start > 0 {
				start = a.Start
			}
			if a.End > 0 {
				end = a.End
			}
			if start < e.First || end > e.Last || start > end {
				return "", fmt.Errorf("读取范围必须在符号 %d-%d 行内", e.First, e.Last)
			}
			code := strings.Join(lines[start-1:end], "\n")
			if len(code) > 12000 {
				return "", fmt.Errorf("符号过长（%d-%d 行），请用 start/end 分段读取", e.First, e.Last)
			}
			data, err := json.Marshal(map[string]any{"symbol": e, "start": start, "end": end, "code": code})
			return string(data), err
		}
	}
	return "", fmt.Errorf("symbol_id 已过期或无效，请重新 list_symbols")
}
