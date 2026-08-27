package creator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// ErrInvalid 输入校验失败（HTTP 400 语义）。
var ErrInvalid = errors.New("creator: invalid input")

// ErrAIUnavailable AI 能力不可用（key 未配置；HTTP 503 语义）。
var ErrAIUnavailable = errors.New("creator: AI 功能不可用（AI_API_KEY 未配置）")

// 输入长度上限（后端不信任客户端输入：超长请求拒绝）。
const (
	maxTopicLen  = 200   // 主题/要点
	maxTitleLen  = 100   // 标题
	maxTags      = 10    // 标签数量
	maxTagLen    = 20    // 单个标签长度
	maxScriptLen = 50000 // 工作副本（AI 脚本正文可能较长）
	maxQueryLen  = 100   // 搜索关键词
)

// 分页默认值（PRD §4.4：默认每页 20 条）。
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// 风格模板 key（CONTEXT.md：default=通用缺省；其余四型）。
var validStyles = map[string]bool{
	"default": true, "ganhuo": true, "juqing": true, "zhongcao": true, "tucao": true,
}

// 作品四态（CONTEXT.md：归档仅作标记，不锁定任何操作）。
var validStatuses = map[string]bool{
	"draft": true, "making": true, "published": true, "archived": true,
}

// 发布状态（T9：待发布 / 已发布）。
var validPubStatuses = map[string]bool{"pending": true, "published": true}

// 发布字段长度上限（后端不信任客户端输入）。
const (
	maxURLlen  = 500 // 发布链接
	maxNoteLen = 500 // 平台备注
)

// CreateWorkRequest 新建作品入参。
type CreateWorkRequest struct {
	Topic string `json:"topic"` // 主题/要点，必填（AI 生成输入）
	Title string `json:"title"` // 标题，可空（AI 生成后回填候选，G9）
}

// PromptSettings 生成提示词覆盖值读取（由 settings.Service 实现，main 注入；
// 避免 creator → settings 直接依赖，与 ai.TimeoutReader 同模式）。
type PromptSettings interface {
	GenerateSystemPrompt(ctx context.Context) (string, error)      // 未设置返回 ""（回退内置默认）
	GenerateUserPromptTemplate(ctx context.Context) (string, error) // 未设置返回 ""（回退内置默认）
}

// Service 作品业务逻辑（HTTP 无关）。
type Service struct {
	store          Store
	ai             AI // 生成能力（T7；可为 nil）
	promptSettings PromptSettings // 提示词覆盖（Issue 18；可 nil = 内置默认）
}

// NewService 构建作品服务；ai 为生成能力（nil 时生成接口返回 ErrAIUnavailable）。
func NewService(store Store, ai AI) *Service {
	return &Service{store: store, ai: ai}
}

// AttachPromptSettings 注入生成提示词覆盖读取（main 装配时调用一次；可 nil）。
func (s *Service) AttachPromptSettings(ps PromptSettings) {
	s.promptSettings = ps
}

// CreateWork 校验并创建草稿作品；初始状态 draft、风格 default、标签空（T4 起可编辑）。
func (s *Service) CreateWork(ctx context.Context, req CreateWorkRequest) (Work, error) {
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		return Work{}, fmt.Errorf("%w: topic is required", ErrInvalid)
	}
	if utf8.RuneCountInString(topic) > maxTopicLen {
		return Work{}, fmt.Errorf("%w: topic too long (max %d)", ErrInvalid, maxTopicLen)
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > maxTitleLen {
		return Work{}, fmt.Errorf("%w: title too long (max %d)", ErrInvalid, maxTitleLen)
	}
	now := db.NowUTC()
	w := Work{
		Title:     title,
		Topic:     topic,
		Style:     "default",
		Tags:      []string{},
		Status:    "draft",
		CreatedAt: now,
		UpdatedAt: now,
	}
	return s.store.CreateWork(ctx, w)
}

// ListWorks 分页查询作品（T5）：状态过滤 + 关键词搜索（标题/主题）+ limit/offset 分页。
// 默认每页 20 条（PRD §4.4），limit 1–100，offset ≥ 0，非法参数返回 ErrInvalid（400）。
func (s *Service) ListWorks(ctx context.Context, f WorkFilter) (WorkPage, error) {
	if f.Limit == 0 {
		f.Limit = defaultPageSize
	}
	if f.Limit < 1 || f.Limit > maxPageSize {
		return WorkPage{}, fmt.Errorf("%w: invalid limit (1-%d)", ErrInvalid, maxPageSize)
	}
	if f.Offset < 0 {
		return WorkPage{}, fmt.Errorf("%w: invalid offset", ErrInvalid)
	}
	if f.Status != "" && !validStatuses[f.Status] {
		return WorkPage{}, fmt.Errorf("%w: invalid status", ErrInvalid)
	}
	f.Query = strings.TrimSpace(f.Query)
	if utf8.RuneCountInString(f.Query) > maxQueryLen {
		return WorkPage{}, fmt.Errorf("%w: query too long (max %d)", ErrInvalid, maxQueryLen)
	}
	items, total, err := s.store.ListWorks(ctx, f)
	if err != nil {
		return WorkPage{}, err
	}
	// 批量附带发布进度摘要（一次查询防 N+1）
	if len(items) > 0 {
		ids := make([]int64, 0, len(items))
		for _, w := range items {
			ids = append(ids, w.ID)
		}
		byWork, err := s.store.ListPublicationsForWorks(ctx, ids)
		if err != nil {
			return WorkPage{}, err
		}
		for i := range items {
			items[i].Publications = byWork[items[i].ID]
		}
	}
	return WorkPage{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// WorkPage 分页响应结构（PRD §4.4：items/total/limit/offset）。
type WorkPage struct {
	Items  []Work `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// GetWork 作品详情（附带发布记录）；不存在返回 ErrNotFound（404）。
func (s *Service) GetWork(ctx context.Context, id int64) (Work, error) {
	w, err := s.store.GetWork(ctx, id)
	if err != nil {
		return Work{}, err
	}
	if err := s.attachPublications(ctx, &w); err != nil {
		return Work{}, err
	}
	return w, nil
}

// UpdateWorkRequest 更新作品入参（全量更新基本信息 + 工作副本）。
type UpdateWorkRequest struct {
	Title  string   `json:"title"`  // 可空
	Topic  string   `json:"topic"`  // 必填
	Style  string   `json:"style"`  // 枚举：default|ganhuo|juqing|zhongcao|tucao
	Status string   `json:"status"` // 枚举：draft|making|published|archived（四态自由切换）
	Tags   []string `json:"tags"`   // ≤10 个，每个 ≤20 字符
	Script string   `json:"script"` // 工作副本（人工编辑内容，不 trim 保格式）
}

// UpdateWork 校验并全量更新作品基本信息与工作副本；刷新 updated_at。
// 归档不锁定操作（T4 验收：归档后仍可编辑/切状态）。
func (s *Service) UpdateWork(ctx context.Context, id int64, req UpdateWorkRequest) (Work, error) {
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		return Work{}, fmt.Errorf("%w: topic is required", ErrInvalid)
	}
	if utf8.RuneCountInString(topic) > maxTopicLen {
		return Work{}, fmt.Errorf("%w: topic too long (max %d)", ErrInvalid, maxTopicLen)
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > maxTitleLen {
		return Work{}, fmt.Errorf("%w: title too long (max %d)", ErrInvalid, maxTitleLen)
	}
	if !validStyles[req.Style] {
		return Work{}, fmt.Errorf("%w: invalid style", ErrInvalid)
	}
	if !validStatuses[req.Status] {
		return Work{}, fmt.Errorf("%w: invalid status", ErrInvalid)
	}
	if len(req.Tags) > maxTags {
		return Work{}, fmt.Errorf("%w: too many tags (max %d)", ErrInvalid, maxTags)
	}
	tags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			return Work{}, fmt.Errorf("%w: empty tag", ErrInvalid)
		}
		if utf8.RuneCountInString(t) > maxTagLen {
			return Work{}, fmt.Errorf("%w: tag too long (max %d)", ErrInvalid, maxTagLen)
		}
		tags = append(tags, t)
	}
	if utf8.RuneCountInString(req.Script) > maxScriptLen {
		return Work{}, fmt.Errorf("%w: script too long (max %d)", ErrInvalid, maxScriptLen)
	}
	return s.store.UpdateWork(ctx, id, Work{
		Title:     title,
		Topic:     topic,
		Style:     req.Style,
		Status:    req.Status,
		Tags:      tags,
		Script:    req.Script,
		UpdatedAt: db.NowUTC(),
	})
}

// DeleteWork 删除作品；不存在返回 ErrNotFound（404）。
// 级联清理：work_versions / work_publications 由外键 ON DELETE CASCADE 处理（T6/T8 建表时带约束）。
func (s *Service) DeleteWork(ctx context.Context, id int64) error {
	return s.store.DeleteWork(ctx, id)
}

// ListPlatforms 平台列表（预置 抖音/B站，按 sort 排序）。
func (s *Service) ListPlatforms(ctx context.Context) ([]Platform, error) {
	return s.store.ListPlatforms(ctx)
}

// ListPublications 作品发布记录列表（懒补启用平台记录后返回，按平台 sort）。
// 作品不存在返回 ErrNotFound（404）。
func (s *Service) ListPublications(ctx context.Context, workID int64) ([]Publication, error) {
	if err := s.store.EnsurePublications(ctx, workID); err != nil {
		return nil, err
	}
	return s.store.ListPublications(ctx, workID)
}

// UpdatePublicationRequest 发布记录更新入参（T9）。
type UpdatePublicationRequest struct {
	Status      string  `json:"status"`                // pending | published
	VersionID   *int64  `json:"versionId"`             // 绑定的版本（可空；须属于该作品）
	URL         string  `json:"url"`                   // 发布链接（选填）
	PublishedAt *string `json:"publishedAt"`           // 发布时间（选填；标记发布未传则自动取当前）
	Note        string  `json:"note"`                  // 平台备注（选填）
}

// UpdatePublication 更新发布记录并应用状态机语义：
//   - status → published：published_at 未提供 → 自动取当前时间；提供则用之（可覆盖）
//   - status → pending：清空 published_at（回退待发布）
//   - version_id 若提供须属于该作品（越权绑定拒绝）；url/note 长度上限校验。
func (s *Service) UpdatePublication(ctx context.Context, workID, pubID int64, req UpdatePublicationRequest) (Publication, error) {
	if !validPubStatuses[req.Status] {
		return Publication{}, fmt.Errorf("%w: invalid publication status", ErrInvalid)
	}
	if len([]rune(req.URL)) > maxURLlen {
		return Publication{}, fmt.Errorf("%w: url too long (max %d)", ErrInvalid, maxURLlen)
	}
	if len([]rune(req.Note)) > maxNoteLen {
		return Publication{}, fmt.Errorf("%w: note too long (max %d)", ErrInvalid, maxNoteLen)
	}
	if req.VersionID != nil {
		// 版本归属校验（该版本必须属于此作品）
		if _, err := s.store.GetVersion(ctx, workID, *req.VersionID); err != nil {
			return Publication{}, fmt.Errorf("%w: version not in work", ErrInvalid)
		}
	}

	pub, err := s.store.GetPublication(ctx, workID, pubID)
	if err != nil {
		return Publication{}, err // 404 语义
	}

	// 状态机：发布时间自动/清空逻辑
	var publishedAt *string
	if req.Status == "published" {
		t := req.PublishedAt
		if t == nil || strings.TrimSpace(*t) == "" {
			now := db.NowUTC()
			t = &now
		}
		publishedAt = t
	} else {
		publishedAt = nil // pending：清空
	}

	pub.Status = req.Status
	pub.VersionID = req.VersionID
	pub.URL = strings.TrimSpace(req.URL)
	pub.PublishedAt = publishedAt
	pub.Note = strings.TrimSpace(req.Note)
	pub.UpdatedAt = db.NowUTC()
	return s.store.UpdatePublication(ctx, pub)
}

// attachPublications 详情附带发布记录（懒补 + 查询）。
func (s *Service) attachPublications(ctx context.Context, w *Work) error {
	pubs, err := s.ListPublications(ctx, w.ID)
	if err != nil {
		return err
	}
	w.Publications = pubs
	return nil
}
