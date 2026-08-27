package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// 审计摘要截断长度（不存全文，个人工具可控成本与体积）。
const summaryLen = 200

// TimeoutReader 超时设置读取（由 settings 包实现，main 注入；避免 ai 依赖具体实现）。
type TimeoutReader interface {
	AITimeoutSeconds(ctx context.Context) (int, error)
}

// ErrNoAPIKey AI key 未配置（HTTP 503 语义；服务正常启动，AI 功能明确报错）。
var ErrNoAPIKey = errors.New("ai: AI_API_KEY 未配置，AI 功能不可用")

// Service AI 编排服务：模块级互斥（一次一个生成任务，PRD §4.5）、
// 超时（默认 180s，settings.ai_timeout_seconds 覆盖）、解析失败自动重试 1 次（换温度，G7）、审计落库。
// API Key 来源：运行时手动配置（apiKeyMgr，网页「AI 设置」写入，重启即失效）；
// provider 字段保留仅为构造兼容，main 装配时恒传 nil（不再支持环境变量注入 key）。
type Service struct {
	provider   Provider       // 兜底 provider（当前恒 nil，保留字段仅为 NewService 签名兼容）
	apiKeyMgr  *APIKeyManager // 运行时手动配置的内存 key 存储（AttachAPIKeyManager 注入；可 nil）
	baseURL    string         // 手动重建 DeepSeek 客户端所用（AI_BASE_URL）
	model      string         // 手动重建 DeepSeek 客户端所用（AI_MODEL）
	audit      *AuditStore
	timeouts   TimeoutReader
	mu         sync.Mutex
}

// NewService 构建 AI 编排服务；provider 可为 nil（key 缺失时 Generate 返回明确错误）。
func NewService(provider Provider, audit *AuditStore, timeouts TimeoutReader) *Service {
	return &Service{provider: provider, audit: audit, timeouts: timeouts}
}

// AttachAPIKeyManager 注入运行时 API Key 管理（main 装配时调用一次）。
// baseURL/model 用于手动配置 key 后动态重建 DeepSeek 客户端；mgr 必须非 nil。
func (s *Service) AttachAPIKeyManager(mgr *APIKeyManager, baseURL, model string) {
	s.apiKeyMgr = mgr
	s.baseURL = baseURL
	s.model = model
}

// GetAPIKeyManager 获取 API Key 管理器（configure 端点写入用）；未注入时返回 nil。
func (s *Service) GetAPIKeyManager() *APIKeyManager { return s.apiKeyMgr }

// EffectiveModel 当前生效的模型名（仅状态展示用，不含任何敏感信息）。
func (s *Service) EffectiveModel() string {
	if m := s.model; m != "" {
		return m
	}
	return DefaultModel
}

// GetEffectiveProvider 返回当前生效的 Provider：内存中手动配置的 key 优先，
// 未配置返回 nil（Generate 报 ErrNoAPIKey → HTTP 503）。
// 手动 key 每次调用重建 client——仅生成路径使用且有模块级互斥，非高频路径开销可忽略。
func (s *Service) GetEffectiveProvider() Provider {
	if s.apiKeyMgr != nil && s.apiKeyMgr.HasAPIKey() {
		if p, err := NewDeepSeekProvider(s.apiKeyMgr.GetAPIKey(), s.baseURL, s.model); err == nil {
			return p
		}
	}
	return s.provider
}

// GenerateResult 一次生成的结果（供调用方落版本/回填）。
type GenerateResult struct {
	Output   GenerateOutput
	Model    string
	Duration time.Duration
}

// Generate 执行一次生成：互斥 → 超时（settings 覆盖）→ 调用 → 解析（失败重试 1 次换温度）→ 审计落库。
// kind 为审计类型（script/topic）；workID 关联作品（无则 0）；输入输出摘要截断落库。
func (s *Service) Generate(ctx context.Context, kind string, workID int64, systemPrompt, userPrompt string) (GenerateResult, error) {
	// 模块级互斥：一次仅一个生成任务（个人工具单任务并发，PRD Q16）
	s.mu.Lock()
	defer s.mu.Unlock()

	provider := s.GetEffectiveProvider()
	if provider == nil {
		err := ErrNoAPIKey
		s.record(ctx, kind, workID, "", "", "", 0, 0, 0, "error", err.Error())
		return GenerateResult{}, err
	}

	timeout := s.effectiveTimeout(ctx)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	// 首次尝试（温度 0.7）
	output, result, err := s.tryGenerate(provider, callCtx, systemPrompt, userPrompt, 0.7)
	// 解析失败自动重试 1 次（换温度 0.2，G7）
	if err != nil {
		output, result, err = s.tryGenerate(provider, callCtx, systemPrompt, userPrompt, 0.2)
	}
	duration := time.Since(start)

	status := "ok"
	errText := ""
	if err != nil {
		status = "error"
		errText = err.Error()
	}
	s.record(ctx, kind, workID, result.Model, truncate(userPrompt), truncate(result.Content), result.PromptTokens, result.CompletionTokens, duration, status, errText)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("ai: generate: %w", err)
	}
	return GenerateResult{Output: output, Model: result.Model, Duration: duration}, nil
}

// tryGenerate 单次调用 + 解析；provider 由 Generate 取定后传入（同一生成内保持一致，避免中途 key 变化）。
func (s *Service) tryGenerate(provider Provider, ctx context.Context, systemPrompt, userPrompt string, temperature float32) (GenerateOutput, ChatResult, error) {
	result, err := provider.Chat(ctx, ChatRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Temperature:  temperature,
	})
	if err != nil {
		return GenerateOutput{}, ChatResult{}, err
	}
	output, err := ParseGenerateOutput(result.Content)
	if err != nil {
		return GenerateOutput{}, result, fmt.Errorf("parse output: %w", err)
	}
	return output, result, nil
}

// effectiveTimeout 读取 settings 覆盖；失败或非法值回退默认 180s。
// 上限 600s 与 settings API 一致；下限放开为 >0（settings 服务已保证 30–600，此处防 0/负值）。
func (s *Service) effectiveTimeout(ctx context.Context) time.Duration {
	timeout := 180 * time.Second
	if s.timeouts != nil {
		if v, err := s.timeouts.AITimeoutSeconds(ctx); err == nil && v > 0 && v <= 600 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return timeout
}

// record 审计落库（成功/失败均记录；落库失败仅记日志不阻塞业务）。
func (s *Service) record(ctx context.Context, kind string, workID int64, model, promptSummary, outputSummary string, promptTokens, completionTokens int, duration time.Duration, status, errText string) {
	if s.audit == nil {
		return
	}
	_, err := s.audit.Insert(ctx, AuditRecord{
		WorkID:           workID,
		Kind:             kind,
		Provider:         "deepseek",
		Model:            model,
		PromptSummary:    promptSummary,
		OutputSummary:    outputSummary,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		DurationMs:       duration.Milliseconds(),
		Status:           status,
		Error:            errText,
		CreatedAt:        db.NowUTC(),
	})
	if err != nil {
		log.Printf("ai: record audit: %v", err)
	}
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) <= summaryLen {
		return s
	}
	return string(r[:summaryLen]) + "…"
}
