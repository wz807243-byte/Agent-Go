package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"example.com/Agent-Go/internal/code"
)

func structureTool(a Args, files []string) (string, error) {
	if os.Getenv("AGENT_AST_GREP") != "1" {
		return "", fmt.Errorf("ast-grep 默认关闭，设置 AGENT_AST_GREP=1 启用")
	}
	if a.Pattern == "" || len(a.Pattern) > 4000 {
		return "", fmt.Errorf("pattern 应为 1-4000 字节")
	}
	v, err := code.CodeVersion(a.Version)
	if err != nil {
		return "", err
	}
	selected, err := scopedFiles(files, a.PathPrefix)
	if err != nil {
		return "", err
	}
	if a.Path != "" {
		allowed := false
		for _, p := range selected {
			if p == a.Path {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("path 不在指定 path_prefix 或可读取文件范围内")
		}
		selected = []string{a.Path}
	}
	selected, err = filesAtVersion(selected, v)
	if err != nil {
		return "", err
	}
	goFiles := selected[:0]
	for _, p := range selected {
		if strings.HasSuffix(p, ".go") {
			goFiles = append(goFiles, p)
		}
	}
	selected = goFiles
	if len(selected) > 50 {
		return "", fmt.Errorf("结构搜索最多50个文件，请缩小 path_prefix 或指定 path")
	}
	exe := os.Getenv("AST_GREP_BIN")
	if exe == "" {
		exe = "ast-grep"
	}
	// Run against stdin in an isolated directory, never discover repository config.
	dir, err := os.MkdirTemp("", "agent-go-ast-grep-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items := []map[string]any{}
	for _, p := range selected {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		src, err := versionRead(p, v, files)
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, exe, "run", "--lang", "go", "--pattern", a.Pattern, "--stdin", "--json=compact")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(src)
		out := cappedBuffer{limit: 256000}
		stderr := cappedBuffer{limit: 2000}
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err = cmd.Run()
		noMatch := false
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && strings.TrimSpace(out.String()) == "[]" && stderr.String() == "" && ctx.Err() == nil {
			noMatch = true
		}
		if err != nil && !noMatch {
			return "", fmt.Errorf("ast-grep 执行失败（请确认已安装）：%w: %s", err, stderr.String())
		}
		if out.overflow {
			return "", fmt.Errorf("结构匹配过多，请缩小范围")
		}
		var matches []struct {
			Text  string `json:"text"`
			Range struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
				End struct {
					Line int `json:"line"`
				} `json:"end"`
			} `json:"range"`
		}
		if err = json.Unmarshal(out.Bytes(), &matches); err != nil {
			return "", fmt.Errorf("ast-grep JSON: %w", err)
		}
		for _, m := range matches {
			text := m.Text
			if len(text) > 1000 {
				text = text[:1000] + " [截断]"
			}
			items = append(items, map[string]any{"path": p, "start": m.Range.Start.Line + 1, "end": m.Range.End.Line + 1, "text": text})
		}
	}
	limit := a.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 30 || a.Cursor < 0 || a.Cursor > len(items) {
		return "", fmt.Errorf("limit 应为1-30，cursor 无效")
	}
	end := min(a.Cursor+limit, len(items))
	next := 0
	if end < len(items) {
		next = end
	}
	data, err := json.Marshal(map[string]any{"version": v, "matches": items[a.Cursor:end], "truncated": end < len(items), "next_cursor": next})
	if len(data) > 24000 {
		return "", fmt.Errorf("结果过长，请减小 limit")
	}
	return string(data), err
}
