package tools

import (
	"os"
)

func Definitions() []any {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	filePath := func() map[string]any {
		return map[string]any{"type": "string", "description": "相对于审查根目录的正斜杠路径，例如 main.go、internal/service.go；禁止绝对路径和反斜杠"}
	}
	num := func() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
	version := func() map[string]any {
		return map[string]any{"type": "string", "enum": []string{"working", "staged", "head"}, "description": "初始默认 working；成功 staged_diff 后默认 staged；可显式指定"}
	}
	page := func() map[string]any {
		return map[string]any{"path_prefix": map[string]any{"type": "string", "description": "相对于审查根目录的正斜杠目录或文件路径；根目录请省略或传空字符串，不能传 Windows 绝对路径"}, "version": version(), "cursor": num(), "limit": num()}
	}
	out := []any{}
	add := func(name, desc string, props map[string]any, required ...string) {
		params := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			params["required"] = required
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": desc, "parameters": params}})
	}
	add("list_files", "列出范围内文件，支持 path_prefix、cursor 分页。truncated 时用 next_cursor 继续。", page())
	search := page()
	search["query"] = str()
	add("search_code", "按版本搜索字面文本，支持目录过滤和匹配分页；检查 skipped 和 truncated。", search, "query")
	add("staged_diff", "读取暂存 Go 变更及前后声明，before 为 HEAD、after 为 staged。", map[string]any{"path": filePath()})
	add("read_file", "按版本读取代码，实际返回包含首尾最多120行，默认80行。超长请求自动分段，返回实际范围、truncated 和 next_start；如需继续，请用 next_start 读取下一段，0表示文件末尾。", map[string]any{"path": filePath(), "version": version(), "start": map[string]any{"type": "integer", "minimum": 1, "description": "起始行号，从1开始，包含此行；继续读取时使用上次返回的 next_start"}, "end": map[string]any{"type": "integer", "minimum": 1, "description": "期望的结束行号，包含此行；超出120行会自动缩短返回范围，必须检查实际范围和 next_start，省略时读取80行"}}, "path")
	if os.Getenv("AGENT_SYMBOLS") != "0" {
		add("list_symbols", "列出 Go 文件中的函数、方法、类型、变量和常量。返回含版本和内容哈希的 symbol_id。", map[string]any{"path": filePath(), "version": version(), "cursor": num(), "limit": num()}, "path")
		add("read_symbol", "读取完整声明；超长声明用文件绝对行号 start/end 分段读取。", map[string]any{"symbol_id": str(), "start": num(), "end": num()}, "symbol_id")
	}
	if os.Getenv("AGENT_AST_GREP") == "1" {
		p := page()
		p["pattern"] = str()
		p["path"] = filePath()
		add("search_structure", "用 ast-grep 搜索 Go 语法结构，仅读取。最多50文件，支持结果分页；匹配不等于缺陷。", p, "pattern")
	}
	return out
}
