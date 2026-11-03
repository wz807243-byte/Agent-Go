package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"example.com/Agent-Go/internal/code"
)

type Args struct {
	Path       string `json:"path"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Query      string `json:"query"`
	Version    string `json:"version"`
	PathPrefix string `json:"path_prefix"`
	Cursor     int    `json:"cursor"`
	Limit      int    `json:"limit"`
	SymbolID   string `json:"symbol_id"`
	Pattern    string `json:"pattern"`
}

func Execute(name string, a Args, files []string) (string, error) {
	var out strings.Builder
	switch name {
	case "list_files", "search_code":
		return pageTool(name, a, files)
	case "list_symbols", "read_symbol":
		if os.Getenv("AGENT_SYMBOLS") == "0" {
			return "", fmt.Errorf("AST 符号工具已关闭")
		}
		return symbolTool(name, a, files)
	case "search_structure":
		return structureTool(a, files)
	case "read_file":
		src, err := versionRead(a.Path, a.Version, files)
		if err != nil {
			return "", err
		}
		if a.Start <= 0 {
			a.Start = 1
		}
		if a.End == 0 {
			a.End = a.Start + 79
		}
		if a.End < a.Start || a.End-a.Start >= 120 {
			return "", fmt.Errorf("每次最多读取 120 行")
		}
		lines := strings.Split(src, "\n")
		if a.Start > len(lines) {
			return "", fmt.Errorf("%s 共 %d 行，请勿继续读取文件末尾之后的范围", a.Path, len(lines))
		}
		v, _ := code.CodeVersion(a.Version)
		fmt.Fprintf(&out, "文件 %s version=%s 共 %d 行；读取范围 %d-%d\n", a.Path, v, len(lines), a.Start, min(a.End, len(lines)))
		for i, line := range lines {
			if i+1 >= a.Start && i+1 <= a.End {
				fmt.Fprintf(&out, "%d %s\n", i+1, line)
			}
		}
	case "staged_diff":
		changes, err := code.InspectSelected(a.Path)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(changes)
		if err != nil {
			return "", err
		}
		if len(data) > 24000 {
			return "", fmt.Errorf("暂存变更上下文超过 24 KB，请分批暂存")
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("未知工具")
	}
	result := out.String()
	if len(result) > 12000 {
		return "", fmt.Errorf("结果超过 12 KB，请缩小读取范围或搜索条件")
	}
	return result, nil
}
