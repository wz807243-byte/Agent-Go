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
		lines := strings.Split(src, "\n")
		if a.Start > len(lines) {
			return "", fmt.Errorf("%s 共 %d 行，请勿继续读取文件末尾之后的范围", a.Path, len(lines))
		}
		if a.End == 0 {
			a.End = a.Start + 79
		}
		if a.End < a.Start {
			return "", fmt.Errorf("结束行号不能小于起始行号：start=%d end=%d", a.Start, a.End)
		}
		requestedEnd := min(a.End, len(lines))
		end := min(requestedEnd, a.Start+119)
		nextStart := 0
		if end < len(lines) {
			nextStart = end + 1
		}
		v, _ := code.CodeVersion(a.Version)
		fmt.Fprintf(&out, "文件 %s version=%s 共 %d 行；读取范围 %d-%d\n", a.Path, v, len(lines), a.Start, end)
		fmt.Fprintf(&out, "truncated=%t next_start=%d（0表示文件末尾）。超长请求已自动限制为最多120行；如需继续，请从 next_start 开始读取。只有上述实际范围已读取。\n", end < requestedEnd, nextStart)
		for i := a.Start - 1; i < end; i++ {
			fmt.Fprintf(&out, "%d %s\n", i+1, lines[i])
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
