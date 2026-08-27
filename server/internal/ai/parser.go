package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// GenerateOutput AI 一次生成的成套内容（PRD §4.5 输出契约：标题候选/脚本/口播稿/标签建议/封面文案）。
type GenerateOutput struct {
	Titles    []string `json:"titles"`
	Script    string   `json:"script"`
	Voiceover string   `json:"voiceover"`
	Tags      []string `json:"tags"`
	CoverCopy string   `json:"cover_copy"`
}

// ParseGenerateOutput 解析模型输出为成套内容。
// 容错策略（G7）：容忍 markdown 围栏与前后杂文本（取首个 { 至末个 }）；
// 字段缺失容忍（零值），但关键字段 script 为空视为解析失败（触发调用方重试）。
func ParseGenerateOutput(raw string) (GenerateOutput, error) {
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return GenerateOutput{}, errors.New("ai: no json object found in output")
	}
	var out GenerateOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return GenerateOutput{}, fmt.Errorf("ai: parse output json: %w", err)
	}
	if strings.TrimSpace(out.Script) == "" {
		return GenerateOutput{}, errors.New("ai: output missing script")
	}
	if out.Titles == nil {
		out.Titles = []string{}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out, nil
}

// extractJSON 提取首个 { 到末个 } 之间的内容（容忍 markdown 围栏/前后说明文字）。
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}
