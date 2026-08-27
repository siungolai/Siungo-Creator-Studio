// Package ai 提供 LLM 调用抽象（PRD §4.5）：
// Provider 接口 + DeepSeek 实现（deepseek-go，MIT）、输出解析容错、模块级互斥、超时（settings 覆盖）、审计落库。
package ai

import (
	"context"
	"errors"
	"fmt"

	"github.com/cohesion-org/deepseek-go"
)

// ChatRequest 单次对话请求（提示词由调用方组装，本层不做业务提示词）。
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float32
}

// ChatResult 单次对话结果（含 token 用量与模型名）。
type ChatResult struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
}

// Provider LLM 提供商抽象：换实现不改调用方（PRD §4.5）。
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResult, error)
}

// 默认模型（AI_MODEL 未配置时）。
// 2026-08-27：deepseek-chat 已被服务端下线（deepseek-go 弃用警告 2026/07/24），
// 改用官方推荐的 deepseek-v4-flash（non-thinking 模式，直接输出 JSON 匹配本项目解析器）。
const DefaultModel = "deepseek-v4-flash"

// DeepSeekProvider 基于 deepseek-go 的实现（API key 仅环境变量注入，不入库不落日志）。
type DeepSeekProvider struct {
	client *deepseek.Client
	model  string
}

// NewDeepSeekProvider 构造 DeepSeek 提供商；key 缺失返回明确错误（AI 功能不可用，其余功能不受影响）。
func NewDeepSeekProvider(apiKey, baseURL, model string) (*DeepSeekProvider, error) {
	if apiKey == "" {
		return nil, errors.New("ai: AI_API_KEY 未配置，AI 功能不可用")
	}
	if model == "" {
		model = DefaultModel
	}
	client := deepseek.NewClient(apiKey)
	if baseURL != "" {
		client = deepseek.NewClient(apiKey, baseURL)
	}
	return &DeepSeekProvider{client: client, model: model}, nil
}

// Chat 发起一次对话。不做流式（PRD Q16）；JSON 结构由提示词强约束 + 解析器容错（见 parser.go）。
func (p *DeepSeekProvider) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	resp, err := p.client.CreateChatCompletion(ctx, &deepseek.ChatCompletionRequest{
		Model: p.model,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: req.SystemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: req.UserPrompt},
		},
		Temperature: req.Temperature,
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("ai: chat: %w", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return ChatResult{}, errors.New("ai: empty chat completion")
	}
	return ChatResult{
		Content:          resp.Choices[0].Message.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}
