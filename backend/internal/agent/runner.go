package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/Agent-Go/internal/code"
	"example.com/Agent-Go/internal/config"
	taskcontext "example.com/Agent-Go/internal/context"
	"example.com/Agent-Go/internal/llm"
	"example.com/Agent-Go/internal/observability"
	"example.com/Agent-Go/internal/review"
	"example.com/Agent-Go/internal/tools"
)

func RunPath(task, target string) (runErr error) {
	originalDir, err := os.Getwd()
	if err != nil {
		return err
	}

	var root string
	var files []string
	scoped := target != ""
	if scoped {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("指定路径不可用: %w", err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if info.IsDir() {
			root = resolved
			target = ""
		} else {
			root = filepath.Dir(resolved)
			target = filepath.Base(resolved)
		}
		if err = os.Chdir(root); err != nil {
			return err
		}
		defer os.Chdir(originalDir)
		if target != "" {
			if !tools.ReviewFile(target) {
				return fmt.Errorf("目前支持 Go 文件、go.mod、任务说明.md 和 README.md")
			}
			files = []string{target}
			if _, err := tools.Read(target, files); err != nil {
				return err
			}
		} else {
			visited := 0
			err = filepath.WalkDir(".", func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				visited++
				if visited > 10000 {
					return fmt.Errorf("目录超过10000项，请指定更小的文件夹")
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if d.IsDir() {
					if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
						return filepath.SkipDir
					}
					return nil
				}
				if tools.ReviewFile(path) {
					files = append(files, filepath.ToSlash(path))
				}
				return nil
			})
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("指定文件夹里没有可审查的 Go 文件")
			}
		}
		task = fmt.Sprintf("%s。只审查指定范围 %s，范围之外的调用方影响标为未确认。", task, absolute)
	} else {
		root, err = code.Git("rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		root = strings.TrimSpace(root)
		if err = os.Chdir(root); err != nil {
			return err
		}
		defer os.Chdir(originalDir)
		files, err = tools.Files()
		if err != nil {
			return err
		}
	}
	endpoint, model, key := config.Agent()
	if key == "" {
		return fmt.Errorf("请通过启动器输入 API Key")
	}
	toolSpecs := tools.Definitions()
	prompt := review.AgentPrompt()
	messages := []any{map[string]string{"role": "system", "content": prompt}, map[string]string{"role": "user", "content": task}}
	context, loaded := taskcontext.TaskContext(files)
	if context != "" {
		messages = append(messages, map[string]string{"role": "user", "content": context})
	}
	record := observability.RunRecord{Started: time.Now(), Project: root, Task: task, Model: model, Status: "failed", UsageComplete: true, Instructions: loaded, Features: map[string]bool{"symbols": os.Getenv("AGENT_SYMBOLS") != "0", "ast_grep": os.Getenv("AGENT_AST_GREP") == "1"}}
	logDir := os.Getenv("AGENT_LOG_DIR")
	if logDir == "" {
		logDir = filepath.Join(originalDir, ".agent-go", "runs")
	}
	totalPrompt, totalCompletion := 0, 0
	defer func() {
		fmt.Fprintf(os.Stderr, "本任务 Token: input=%d output=%d\n", totalPrompt, totalCompletion)
		record.DurationMS = time.Since(record.Started).Milliseconds()
		record.Input = totalPrompt
		record.Output = totalCompletion
		record.Total = totalPrompt + totalCompletion
		if runErr != nil {
			record.Error = runErr.Error()
			record.UsageComplete = false
		}
		if path, err := observability.SaveRun(logDir, record); err != nil {
			fmt.Fprintf(os.Stderr, "运行记录保存失败: %v\n", err)
		} else {
			fmt.Printf("运行记录: %s\n", path)
		}
		if runErr != nil {
			fmt.Printf("任务未完成：%v。已执行工具见上方，不能据此认定代码无问题。\n", runErr)
		}
	}()
	fmt.Printf("任务：%s\n项目：%s\n", task, strings.TrimSpace(root))
	cache := map[string]string{} // Successful operation signature -> original tool_call_id.
	defaultVersion := "working"
	idleRounds := 0
	summarize := false
	reportOnly := false
	reportTokens := taskcontext.ReportCompletionTokens
	sent := 0
	// A truncated response may use one extra request, even after the last round.
	for step := 0; step < taskcontext.MaxRounds+taskcontext.MaxReportRetries; step++ {
		finalRound := reportOnly || summarize || step >= taskcontext.MaxRounds-1 || sent > taskcontext.SummaryBytes
		maxTokens := taskcontext.MaxCompletionTokens
		request := map[string]any{"model": model, "messages": messages, "tools": toolSpecs}
		if finalRound {
			if !reportOnly {
				messages = append(messages, map[string]string{"role": "user", "content": "现在结束探索，依据已有证据汇报已检查范围、确认的问题和未检查部分。合并同一根因，简洁给出行号、触发条件、影响和修复建议。不要再调用工具，不要将假设当作缺陷。"})
			}
			reportOnly = true
			maxTokens = reportTokens
			request["messages"] = messages
			request["tool_choice"] = "none"
			fmt.Fprintln(os.Stderr, "进入汇报轮次...")
		}
		request["max_tokens"] = maxTokens
		body, err := json.Marshal(request)
		if err != nil {
			return err
		}
		sent += len(body)
		if len(body) > taskcontext.MaxRequestBytes || sent > taskcontext.MaxTotalBytes {
			return fmt.Errorf("达到上下文预算，审查未完成")
		}
		if record.ReportRetries > 0 {
			fmt.Fprintf(os.Stderr, "Agent 汇报重试 %d/%d（输出上限 %d tokens）...\n", record.ReportRetries, taskcontext.MaxReportRetries, maxTokens)
		} else {
			fmt.Fprintf(os.Stderr, "Agent 第 %d/%d 轮分析...\n", step+1, taskcontext.MaxRounds)
		}
		resp, err := llm.Post(endpoint, key, body)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2000001))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("模型服务返回 %s", resp.Status)
		}
		if len(data) > 2000000 {
			return fmt.Errorf("模型响应过大")
		}
		var reply struct {
			Choices []struct {
				Message json.RawMessage `json:"message"`
				Finish  string          `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err = json.Unmarshal(data, &reply); err != nil {
			return err
		}
		var envelope struct {
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(data, &envelope); err == nil {
			record.Rounds = append(record.Rounds, envelope.Usage)
			var counts struct {
				Prompt *int `json:"prompt_tokens"`
				Output *int `json:"completion_tokens"`
			}
			if json.Unmarshal(envelope.Usage, &counts) != nil || counts.Prompt == nil || counts.Output == nil {
				record.UsageComplete = false
			}
		}
		totalPrompt += reply.Usage.Prompt
		totalCompletion += reply.Usage.Completion
		if len(reply.Choices) == 0 {
			return fmt.Errorf("模型未返回内容")
		}
		choice := reply.Choices[0]
		var msg struct {
			Content string      `json:"content"`
			Calls   []agentCall `json:"tool_calls"`
		}
		if err = json.Unmarshal(choice.Message, &msg); err != nil {
			return err
		}
		if choice.Finish == "length" {
			if strings.TrimSpace(msg.Content) != "" {
				record.PartialReports = append(record.PartialReports, msg.Content)
				fmt.Printf("部分报告（输出被截断，内容不完整，不能视为已完成审查）：\n%s\n", msg.Content)
			}
			if record.ReportRetries >= taskcontext.MaxReportRetries {
				return fmt.Errorf("模型输出再次达到长度上限（%d tokens），已停止重试；已有正文已标为不完整并保存在运行记录 partial_reports 中", maxTokens)
			}
			record.ReportRetries++
			if finalRound {
				reportTokens = taskcontext.ReportRetryCompletionTokens
			}
			reportOnly = true
			// Do not append an incomplete assistant/tool-call message. The original
			// evidence remains available; ask for a complete, shorter replacement.
			messages = append(messages, map[string]string{"role": "user", "content": "上一次输出达到长度上限，被截断，尚未完成审查。现在停止探索，仅依据上文已有代码和工具证据重新给出完整、简洁的报告，不要续接半句，不要再调用工具。合并同一根因，保留已确认问题的行号、触发条件、影响和修复建议，明确未检查范围，不要把未确认疑点当作缺陷。"})
			fmt.Fprintf(os.Stderr, "模型输出达到 %d tokens 上限，将仅重试汇报（输出上限 %d tokens）。\n", maxTokens, reportTokens)
			continue
		}
		if choice.Finish != "stop" && choice.Finish != "tool_calls" {
			return fmt.Errorf("模型未完成分析: %s", choice.Finish)
		}
		if len(msg.Calls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				return fmt.Errorf("模型返回空回答")
			}
			record.Report = msg.Content
			record.Status = "reported"
			fmt.Println(msg.Content)
			return nil
		}
		if finalRound {
			return fmt.Errorf("模型在汇报轮仍请求工具，审查未完成")
		}
		if len(msg.Calls) > 4 {
			return fmt.Errorf("单轮工具调用超过4次，审查未完成")
		}
		messages = append(messages, choice.Message)
		progressed := false
		for _, call := range msg.Calls {
			var args tools.Args
			cached := false
			err := json.Unmarshal([]byte(call.Function.Arguments), &args)
			result := ""
			if err == nil {
				if call.Function.Name == "read_symbol" {
					var ref code.SymbolRef
					ref, err = code.ParseSymbolID(args.SymbolID)
					args.Version = ref.Version
				}
				if args.Version == "" {
					args.Version = defaultVersion
				}
				if call.Function.Name == "staged_diff" {
					args.Version = "staged"
				}
				if scoped && args.Version != "working" {
					err = fmt.Errorf("指定路径模式仅支持 working 版本")
				}
			}
			if err == nil {
				if target != "" && call.Function.Name == "staged_diff" {
					args.Path = target
				}
				if call.Function.Name == "read_file" {
					if args.Start <= 0 {
						args.Start = 1
					}
					if args.End == 0 {
						args.End = args.Start + 79
					}
				}
				canonical, _ := json.Marshal(args)
				signature := call.Function.Name + string(canonical)
				fmt.Printf("工具 %s %s\n", call.Function.Name, canonical)
				if previous, ok := cache[signature]; ok {
					result = "缓存命中：结果与先前工具调用 " + previous + " 相同，请使用此前完整结果。"
					cached = true
					fmt.Println("复用成功操作的缓存结果。")
				} else {
					if scoped && call.Function.Name == "staged_diff" {
						err = fmt.Errorf("指定路径模式仅审查 working 版本，请使用其他读取工具")
					} else {
						result, err = tools.Execute(call.Function.Name, args, files)
					}
					if err == nil {
						cache[signature] = call.ID
						if call.Function.Name == "staged_diff" {
							defaultVersion = "staged"
						}
						progressed = true
						fmt.Printf("工具完成，返回 %d 字节\n", len(result))
					}
				}
			}
			if err != nil {
				result = "工具错误: " + err.Error()
				fmt.Println(result)
			}
			record.Tools = append(record.Tools, observability.ToolRecord{Name: call.Function.Name, Arguments: args, Cached: cached, Bytes: len(result), Failed: err != nil})
			messages = append(messages, map[string]string{"role": "tool", "tool_call_id": call.ID, "content": result})
		}
		if progressed {
			idleRounds = 0
		} else {
			idleRounds++
		}
		summarize = idleRounds >= 2
	}
	return fmt.Errorf("达到8轮上限，审查未完成，请缩小任务范围")
}
