package creator

import (
	"context"
	"fmt"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/ai"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// AI 生成能力接口（ai.Service 实现；接口便于单测 mock，依赖方向 creator → ai 无环）。
type AI interface {
	Generate(ctx context.Context, kind string, workID int64, systemPrompt, userPrompt string) (ai.GenerateResult, error)
}

// GenerateRequest 生成请求（T7）：平台维度 + 可选主题/风格覆盖。
type GenerateRequest struct {
	Platform string `json:"platform"` // '' 通用 | douyin | bilibili
	Topic    string `json:"topic"`    // 可选：覆盖作品主题（空 = 用作品当前值）
	Style    string `json:"style"`    // 可选：覆盖作品风格（空 = 用作品当前值）
}

// 平台维度（PRD Q19/G 系列：通用/抖音/B站）。
var validPlatforms = map[string]bool{"": true, "douyin": true, "bilibili": true}

// GenerateVersion 生成新版本并激活（T7 验收）：
// 校验 → 读取作品（404）→ 提示词组装（风格/平台微调）→ AI 生成（失败不产生版本，502 语义）
// → 事务落版本 + 激活：标题回填（空则 titles[0]，G9）、副本刷新、状态置 making。
func (s *Service) GenerateVersion(ctx context.Context, workID int64, req GenerateRequest) (Version, Work, error) {
	if !validPlatforms[req.Platform] {
		return Version{}, Work{}, fmt.Errorf("%w: invalid platform", ErrInvalid)
	}
	if req.Style != "" && !validStyles[req.Style] {
		return Version{}, Work{}, fmt.Errorf("%w: invalid style", ErrInvalid)
	}
	topic := req.Topic
	if topic != "" && len([]rune(topic)) > maxTopicLen {
		return Version{}, Work{}, fmt.Errorf("%w: topic too long (max %d)", ErrInvalid, maxTopicLen)
	}

	work, err := s.store.GetWork(ctx, workID)
	if err != nil {
		return Version{}, Work{}, err // ErrNotFound 由 HTTP 层映射 404
	}
	if topic == "" {
		topic = work.Topic
	}
	style := req.Style
	if style == "" {
		style = work.Style
	}

	sysPrompt, userPrompt := s.buildGeneratePrompts(ctx, topic, style, req.Platform)
	if s.ai == nil {
		return Version{}, Work{}, ErrAIUnavailable
	}
	result, err := s.ai.Generate(ctx, "script", workID, sysPrompt, userPrompt)
	if err != nil {
		return Version{}, Work{}, err // 502 语义（HTTP 层映射）
	}

	// 标题回填（G9）：作品标题为空 → 候选第一条；已有标题不改
	title := work.Title
	if title == "" && len(result.Output.Titles) > 0 {
		title = result.Output.Titles[0]
	}

	now := db.NowUTC()
	v := Version{
		WorkID:    workID,
		Platform:  req.Platform,
		Content:   result.Output,
		Model:     result.Model,
		CreatedAt: now,
	}
	// 生成即激活：副本刷新为新版本脚本快照
	w := Work{
		ID:              workID,
		Title:           title,
		Script:          result.Output.Script,
		Status:          "making",
		ActiveVersionID: &v.ID,
		UpdatedAt:       now,
	}
	return s.store.SaveGenerated(ctx, v, w)
}

// ListVersions 版本历史（倒序；归属校验由 store 的 work_id 条件保证）。
func (s *Service) ListVersions(ctx context.Context, workID int64) ([]Version, error) {
	// 校验作品存在（404 语义）
	if _, err := s.store.GetWork(ctx, workID); err != nil {
		return nil, err
	}
	return s.store.ListVersions(ctx, workID)
}

// GetVersion 读取指定版本（归属校验；不存在 ErrNotFound）。
func (s *Service) GetVersion(ctx context.Context, workID, versionID int64) (Version, error) {
	return s.store.GetVersion(ctx, workID, versionID)
}

// ActivateVersion 选用版本：刷新工作副本 + 记录来源版本（G4：版本不可变，仅可选用）。
func (s *Service) ActivateVersion(ctx context.Context, workID, versionID int64) (Work, error) {
	if _, err := s.store.GetWork(ctx, workID); err != nil {
		return Work{}, err
	}
	return s.store.ActivateVersion(ctx, workID, versionID)
}
