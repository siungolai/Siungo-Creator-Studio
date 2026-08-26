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
)

// ErrNotFound 目标不存在（HTTP 404 语义）。
var ErrNotFound = errors.New("creator: not found")

// Work 作品实体（works 表）。Tags 以 JSON 数组文本落库。
type Work struct {
	ID              int64    `json:"id"`
	Title           string   `json:"title"`
	Topic           string   `json:"topic"`
	Style           string   `json:"style"`
	Script          string   `json:"script,omitempty"`
	ActiveVersionID *int64   `json:"activeVersionId,omitempty"`
	Tags            []string `json:"tags"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

// Platform 发布平台（platforms 表）。
type Platform struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Sort    int    `json:"sort"`
	Enabled bool   `json:"enabled"`
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

// 编译期断言：SQLiteStore 实现 Store。
var _ Store = (*SQLiteStore)(nil)
