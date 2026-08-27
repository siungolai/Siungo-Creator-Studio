// Package creator 提供作品（work）领域：数据访问（store）、业务（service）、HTTP（handlers）三层分离。
// 领域词汇见 CONTEXT.md：作品 / 主题 / 平台。本文件为数据访问层。
package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// ErrNotFound 目标不存在（HTTP 404 语义）。
var ErrNotFound = errors.New("creator: not found")

// Work 作品实体（works 表）。Tags 以 JSON 数组文本落库。
// Publications 为发布进度摘要（T9 列表/详情附带，按平台 sort；非表列）。
type Work struct {
	ID              int64         `json:"id"`
	Title           string        `json:"title"`
	Topic           string        `json:"topic"`
	Style           string        `json:"style"`
	Script          string        `json:"script,omitempty"`
	ActiveVersionID *int64        `json:"activeVersionId,omitempty"`
	Tags            []string      `json:"tags"`
	Status          string        `json:"status"`
	CreatedAt       string        `json:"createdAt"`
	UpdatedAt       string        `json:"updatedAt"`
	Publications    []Publication `json:"publications,omitempty"`
}

// Platform 发布平台（platforms 表）。
type Platform struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Sort    int    `json:"sort"`
	Enabled bool   `json:"enabled"`
}

// Publication 发布记录（work_publications 表；作品×平台唯一，T9）。
// 状态机：pending（待发布）↔ published（已发布）；回退待发布清空发布时间。
type Publication struct {
	ID           int64   `json:"id"`
	WorkID       int64   `json:"workId"`
	PlatformID   string  `json:"platformId"`
	PlatformName string  `json:"platformName"` // JOIN platforms.name（展示用）
	VersionID    *int64  `json:"versionId,omitempty"`
	Status       string  `json:"status"` // pending | published
	URL          string  `json:"url,omitempty"`
	PublishedAt  *string `json:"publishedAt,omitempty"`
	Note         string  `json:"note,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

// WorkFilter 列表查询条件（T5：状态过滤 + 关键词搜索 + 分页）。
type WorkFilter struct {
	Status string // 空 = 全部
	Query  string // 标题/主题模糊匹配（已 trim）
	Limit  int
	Offset int
}

// Store 作品存储抽象（SQLite 实现；接口便于单测替换）。
type Store interface {
	ListWorks(ctx context.Context, f WorkFilter) ([]Work, int, error) // items + total（updated_at DESC）
	CreateWork(ctx context.Context, w Work) (Work, error)
	GetWork(ctx context.Context, id int64) (Work, error)
	UpdateWork(ctx context.Context, id int64, w Work) (Work, error) // 不存在返回 ErrNotFound
	DeleteWork(ctx context.Context, id int64) error                 // 不存在返回 ErrNotFound
	ListPlatforms(ctx context.Context) ([]Platform, error)
	// 版本（T7）
	CreateVersion(ctx context.Context, v Version) (Version, error)
	ListVersions(ctx context.Context, workID int64) ([]Version, error)
	GetVersion(ctx context.Context, workID, versionID int64) (Version, error) // 归属校验；不存在 ErrNotFound
	SaveGenerated(ctx context.Context, v Version, w Work) (Version, Work, error) // 事务：版本入库 + 作品激活
	ActivateVersion(ctx context.Context, workID, versionID int64) (Work, error)  // 事务：副本刷新 + active_version_id
	// 发布（T9）
	EnsurePublications(ctx context.Context, workID int64) error              // 懒补启用平台记录（幂等）
	ListPublications(ctx context.Context, workID int64) ([]Publication, error) // JOIN 平台名，按 sort
	GetPublication(ctx context.Context, workID, pubID int64) (Publication, error) // 归属校验；不存在 ErrNotFound
	UpdatePublication(ctx context.Context, p Publication) (Publication, error)  // 不存在 ErrNotFound
	ListPublicationsForWorks(ctx context.Context, workIDs []int64) (map[int64][]Publication, error) // 列表页批量摘要
}

// SQLiteStore 基于 database/sql 的作品存储（表由 db 包迁移创建）。
type SQLiteStore struct {
	conn *sql.DB
}

// NewSQLiteStore 用已迁移的数据库连接构建作品存储。
func NewSQLiteStore(conn *sql.DB) *SQLiteStore {
	return &SQLiteStore{conn: conn}
}

const workColumns = `id, title, topic, style, script, active_version_id, tags, status, created_at, updated_at`

func scanWork(row interface{ Scan(...any) error }) (Work, error) {
	var w Work
	var tags string
	var activeVersionID sql.NullInt64
	err := row.Scan(&w.ID, &w.Title, &w.Topic, &w.Style, &w.Script, &activeVersionID, &tags, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Work{}, err
	}
	if activeVersionID.Valid {
		w.ActiveVersionID = &activeVersionID.Int64
	}
	// tags 列保证为合法 JSON 数组（默认 '[]'）；解析失败降级为空数组不阻断读取
	if err := json.Unmarshal([]byte(tags), &w.Tags); err != nil || w.Tags == nil {
		w.Tags = []string{}
	}
	return w, nil
}

// ListWorks 按条件查询作品：items + total。条件为空时全量；instr 匹配避免 LIKE 通配符注入。
func (s *SQLiteStore) ListWorks(ctx context.Context, f WorkFilter) ([]Work, int, error) {
	where := ""
	args := []any{}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Query != "" {
		where += " AND (instr(lower(title), lower(?)) > 0 OR instr(lower(topic), lower(?)) > 0)"
		args = append(args, f.Query, f.Query)
	}
	if where != "" {
		where = " WHERE " + strings.TrimPrefix(where, " AND ")
	}

	var total int
	if err := s.conn.QueryRowContext(ctx, `SELECT count(*) FROM works`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count works: %w", err)
	}

	queryArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := s.conn.QueryContext(ctx,
		`SELECT `+workColumns+` FROM works`+where+` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`,
		queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list works: %w", err)
	}
	defer rows.Close()
	// 空列表返回 [] 而非 null（JSON 一致性）
	works := []Work{}
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan work: %w", err)
		}
		works = append(works, w)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list works: %w", err)
	}
	return works, total, nil
}

func (s *SQLiteStore) CreateWork(ctx context.Context, w Work) (Work, error) {
	tags, err := json.Marshal(w.Tags)
	if err != nil {
		return Work{}, fmt.Errorf("marshal tags: %w", err)
	}
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO works (title, topic, style, script, tags, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Title, w.Topic, w.Style, w.Script, string(tags), w.Status, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		return Work{}, fmt.Errorf("create work: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Work{}, fmt.Errorf("create work id: %w", err)
	}
	w.ID = id
	return w, nil
}

func (s *SQLiteStore) GetWork(ctx context.Context, id int64) (Work, error) {
	row := s.conn.QueryRowContext(ctx, `SELECT `+workColumns+` FROM works WHERE id = ?`, id)
	w, err := scanWork(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Work{}, ErrNotFound
	}
	if err != nil {
		return Work{}, fmt.Errorf("get work: %w", err)
	}
	return w, nil
}

// UpdateWork 全量更新基本信息与工作副本并刷新 updated_at；返回更新后的完整作品。
func (s *SQLiteStore) UpdateWork(ctx context.Context, id int64, w Work) (Work, error) {
	tags, err := json.Marshal(w.Tags)
	if err != nil {
		return Work{}, fmt.Errorf("marshal tags: %w", err)
	}
	res, err := s.conn.ExecContext(ctx,
		`UPDATE works SET title = ?, topic = ?, style = ?, script = ?, tags = ?, status = ?, updated_at = ? WHERE id = ?`,
		w.Title, w.Topic, w.Style, w.Script, string(tags), w.Status, w.UpdatedAt, id)
	if err != nil {
		return Work{}, fmt.Errorf("update work: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Work{}, fmt.Errorf("update work rows: %w", err)
	}
	if n == 0 {
		return Work{}, ErrNotFound
	}
	return s.GetWork(ctx, id)
}

func (s *SQLiteStore) DeleteWork(ctx context.Context, id int64) error {
	res, err := s.conn.ExecContext(ctx, `DELETE FROM works WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete work: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete work rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListPlatforms(ctx context.Context) ([]Platform, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT id, name, sort, enabled FROM platforms ORDER BY sort ASC`)
	if err != nil {
		return nil, fmt.Errorf("list platforms: %w", err)
	}
	defer rows.Close()
	platforms := []Platform{}
	for rows.Next() {
		var p Platform
		if err := rows.Scan(&p.ID, &p.Name, &p.Sort, &p.Enabled); err != nil {
			return nil, fmt.Errorf("scan platform: %w", err)
		}
		platforms = append(platforms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list platforms: %w", err)
	}
	return platforms, nil
}

// ---- 发布（T9）----

const publicationColumns = `p.id, p.work_id, p.platform_id, pl.name, p.version_id, p.status, p.url, p.published_at, p.note, p.created_at, p.updated_at`

func scanPublication(row interface{ Scan(...any) error }) (Publication, error) {
	var p Publication
	var versionID sql.NullInt64
	var publishedAtStr sql.NullString
	// version_id 与 published_at 均可能为 NULL
	if err := row.Scan(&p.ID, &p.WorkID, &p.PlatformID, &p.PlatformName,
		&versionID, &p.Status, &p.URL, &publishedAtStr, &p.Note, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Publication{}, err
	}
	if versionID.Valid {
		p.VersionID = &versionID.Int64
	}
	if publishedAtStr.Valid {
		s := publishedAtStr.String
		p.PublishedAt = &s
	}
	return p, nil
}

// EnsurePublications 为作品懒补启用平台的发布记录（INSERT OR IGNORE，幂等）。
// 新作品创建时即补；存量作品（迁移前创建）首次访问时自动补齐。
func (s *SQLiteStore) EnsurePublications(ctx context.Context, workID int64) error {
	// 先确认作品存在（避免孤儿记录）
	var one int
	if err := s.conn.QueryRowContext(ctx, `SELECT 1 FROM works WHERE id = ?`, workID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("ensure publications: check work: %w", err)
	}
	_, err := s.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO work_publications (work_id, platform_id, status, created_at, updated_at)
		 SELECT ?, id, 'pending', ?, ? FROM platforms WHERE enabled = 1`,
		workID, db.NowUTC(), db.NowUTC())
	if err != nil {
		return fmt.Errorf("ensure publications: %w", err)
	}
	return nil
}

// ListPublications 发布记录列表（JOIN 平台名，按平台 sort 排序）。
func (s *SQLiteStore) ListPublications(ctx context.Context, workID int64) ([]Publication, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT `+publicationColumns+` FROM work_publications p
		 JOIN platforms pl ON pl.id = p.platform_id
		 WHERE p.work_id = ? ORDER BY pl.sort ASC`, workID)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	defer rows.Close()
	pubs := []Publication{}
	for rows.Next() {
		p, err := scanPublication(rows)
		if err != nil {
			return nil, fmt.Errorf("scan publication: %w", err)
		}
		pubs = append(pubs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	return pubs, nil
}

// GetPublication 单条发布记录（归属校验：work_id + id）。
func (s *SQLiteStore) GetPublication(ctx context.Context, workID, pubID int64) (Publication, error) {
	row := s.conn.QueryRowContext(ctx,
		`SELECT `+publicationColumns+` FROM work_publications p
		 JOIN platforms pl ON pl.id = p.platform_id
		 WHERE p.work_id = ? AND p.id = ?`, workID, pubID)
	p, err := scanPublication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Publication{}, ErrNotFound
	}
	if err != nil {
		return Publication{}, fmt.Errorf("get publication: %w", err)
	}
	return p, nil
}

// UpdatePublication 全量更新发布记录并刷新 updated_at；返回更新后的完整记录。
func (s *SQLiteStore) UpdatePublication(ctx context.Context, p Publication) (Publication, error) {
	var versionID any
	if p.VersionID != nil {
		versionID = *p.VersionID
	}
	var publishedAt any
	if p.PublishedAt != nil {
		publishedAt = *p.PublishedAt
	}
	res, err := s.conn.ExecContext(ctx,
		`UPDATE work_publications SET version_id = ?, status = ?, url = ?, published_at = ?, note = ?, updated_at = ?
		 WHERE id = ? AND work_id = ?`,
		versionID, p.Status, p.URL, publishedAt, p.Note, p.UpdatedAt, p.ID, p.WorkID)
	if err != nil {
		return Publication{}, fmt.Errorf("update publication: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Publication{}, fmt.Errorf("update publication rows: %w", err)
	}
	if n == 0 {
		return Publication{}, ErrNotFound
	}
	return s.GetPublication(ctx, p.WorkID, p.ID)
}

// ListPublicationsForWorks 批量查询多个作品的发布记录（列表页进度摘要，一次查询防 N+1）。
// 结果按 work_id 分组，组内按平台 sort 排序。
func (s *SQLiteStore) ListPublicationsForWorks(ctx context.Context, workIDs []int64) (map[int64][]Publication, error) {
	if len(workIDs) == 0 {
		return map[int64][]Publication{}, nil
	}
	// 动态占位符（IN 子句；调用方为内部查询，ID 列表受分页大小限制 ≤100）
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workIDs)), ",")
	args := make([]any, 0, len(workIDs))
	for _, id := range workIDs {
		args = append(args, id)
	}
	rows, err := s.conn.QueryContext(ctx,
		`SELECT `+publicationColumns+` FROM work_publications p
		 JOIN platforms pl ON pl.id = p.platform_id
		 WHERE p.work_id IN (`+placeholders+`) ORDER BY p.work_id ASC, pl.sort ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list publications for works: %w", err)
	}
	defer rows.Close()
	byWork := map[int64][]Publication{}
	for rows.Next() {
		p, err := scanPublication(rows)
		if err != nil {
			return nil, fmt.Errorf("scan publication batch: %w", err)
		}
		byWork[p.WorkID] = append(byWork[p.WorkID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list publications for works: %w", err)
	}
	return byWork, nil
}

// 编译期断言：SQLiteStore 实现 Store。
var _ Store = (*SQLiteStore)(nil)
