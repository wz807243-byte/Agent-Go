package review

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"example.com/Agent-Go/internal/code"
	"example.com/Agent-Go/internal/config"
	"example.com/Agent-Go/internal/llm"
)

func Run(changes []code.Change) error {
	if len(changes) == 0 {
		fmt.Println("暂存区没有 Go 文件变更")
		return nil
	}
	data, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	if len(data) > 48000 {
		return fmt.Errorf("变更数据超过 48000 字节，本次未审查。请分批暂存")
	}
	endpoint, model, key := config.Review()
	if endpoint == "https://api.deepseek.com/chat/completions" && key == "" {
		return fmt.Errorf("请在当前终端设置 REVIEW_API_KEY")
	}
	if endpoint == "" || model == "" {
		return fmt.Errorf("请配置 REVIEW_URL（完整 chat/completions 地址）和 REVIEW_MODEL")
	}
	prompt := DiffPrompt
	body, err := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "system", "content": prompt}, {"role": "user", "content": string(data)}}})
	if err != nil {
		return err
	}
	resp, err := llm.Post(endpoint, key, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("模型服务返回 %s", resp.Status)
	}
	var reply struct {
		Choices []struct {
			Message       struct{ Content string }
			Finish_reason string
		}
		Usage json.RawMessage
	}
	if err = json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return err
	}
	if len(reply.Choices) == 0 {
		return fmt.Errorf("模型未返回审查内容")
	}
	msg := reply.Choices[0]
	if msg.Finish_reason != "" && msg.Finish_reason != "stop" {
		return fmt.Errorf("模型未完成审查: %s", msg.Finish_reason)
	}
	var report struct {
		Findings []Finding       `json:"findings"`
		Usage    json.RawMessage `json:"usage"`
	}
	if err = json.Unmarshal([]byte(msg.Message.Content), &report); err != nil || report.Findings == nil {
		return fmt.Errorf("模型返回的审查 JSON 格式错误")
	}
	for _, f := range report.Findings {
		valid := false
		for _, c := range changes {
			if c.File != f.File {
				continue
			}
			side := 1
			if f.Side == "after" {
				side = 3
			} else if f.Side != "before" {
				continue
			}
			valid = code.Touched(f.Line, f.Line, code.Hunk.FindAllStringSubmatch(c.Diff, -1), side)
		}
		if !valid || strings.TrimSpace(f.Reason) == "" {
			return fmt.Errorf("审查意见位置或理由无效: %s:%d", f.File, f.Line)
		}
	}
	report.Usage = reply.Usage
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
