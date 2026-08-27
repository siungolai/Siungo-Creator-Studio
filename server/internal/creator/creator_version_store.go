package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/ai"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// Version 作品版本（work_versions 表；CONTEXT.md：不可变、只增不删，仅可"选用"）。
type Version struct {
	ID        int64             `json:"id"`
	WorkID    int64             `json:"workId"`
	Platform  string            `json:"platform"` // '' 通用 | douyin | bilibili
	Content   ai.GenerateOutput `json:"content"`
	Model     string            `json:"model"`
	CreatedAt string            `json:"createdAt"`
}

func versionColumns() string {
	return `id, work_id, platform, content_json, model, created_at`
}

func scanVersion(row interface{ Scan(...any) error }) (Version, error) {
	var v Version
	var contentJSON string
	err := row.Scan(&v.ID, &v.WorkID, &v.Platform, &contentJSON, &v.Model, &v.CreatedAt)
	if err != nil {
		return Version{}, err
	}
	// content_json 解析失败降级为空输出（数据异常不应阻断列表）
	if err := json.Unmarshal([]byte(contentJSON), &v.Content); err != nil {
		v.Content = ai.GenerateOutput{Titles: []string{}, Tags: []string{}}
	}
	return v, nil
}

// CreateVersion 插入版本（调用方已校验归属）。
func (s *SQLiteStore) CreateVersion(ctx context.Context, v Version) (Version, error) {
	contentJSON, err := json.Marshal(v.Content)
	if err != nil {
		return Version{}, fmt.Errorf("marshal version content: %w", err)
	}
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO work_versions (work_id, platform, content_json, model, created_at) VALUES (?, ?, ?, ?, ?)`,
		v.WorkID, v.Platform, string(contentJSON), v.Model, v.CreatedAt)
	if err != nil {
		return Version{}, fmt.Errorf("create version: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Version{}, fmt.Errorf("create version id: %w", err)
	}
	v.ID = id
	return v, nil
}

// ListVersions 版本列表（created_at 倒序；按平台分类由展示层处理）。
func (s *SQLiteStore) ListVersions(ctx context.Context, workID int64) ([]Version, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT `+versionColumns()+` FROM work_versions WHERE work_id = ? ORDER BY created_at DESC, id DESC`, workID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	versions := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	return versions, nil
}

// GetVersion 按归属读取版本（work_id + id 双条件，跨作品访问返回 ErrNotFound）。
func (s *SQLiteStore) GetVersion(ctx context.Context, workID, versionID int64) (Version, error) {
	row := s.conn.QueryRowContext(ctx,
		`SELECT `+versionColumns()+` FROM work_versions WHERE id = ? AND work_id = ?`, versionID, workID)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	if err != nil {
		return Version{}, fmt.Errorf("get version: %w", err)
	}
	return v, nil
}

// SaveGenerated 事务：版本入库 + 作品激活（标题回填/副本刷新/状态/updated_at）。
// 保证生成结果与作品状态要么同时生效、要么都不生效。
func (s *SQLiteStore) SaveGenerated(ctx context.Context, v Version, w Work) (Version, Work, error) {
	contentJSON, err := json.Marshal(v.Content)
	if err != nil {
		return Version{}, Work{}, fmt.Errorf("marshal version content: %w", err)
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO work_versions (work_id, platform, content_json, model, created_at) VALUES (?, ?, ?, ?, ?)`,
		v.WorkID, v.Platform, string(contentJSON), v.Model, v.CreatedAt)
	if err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated insert version: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated id: %w", err)
	}
	v.ID = id

	if _, err := tx.ExecContext(ctx,
		`UPDATE works SET title = ?, script = ?, active_version_id = ?, status = ?, updated_at = ? WHERE id = ?`,
		w.Title, w.Script, id, w.Status, w.UpdatedAt, w.ID); err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated update work: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated commit: %w", err)
	}
	work, err := s.GetWork(ctx, w.ID)
	if err != nil {
		return Version{}, Work{}, fmt.Errorf("save generated get work: %w", err)
	}
	return v, work, nil
}

// ActivateVersion 事务：将指定版本设为当前（works.script = 版本脚本快照 + active_version_id + updated_at）。
// 版本不可变；激活不修改版本本身。
func (s *SQLiteStore) ActivateVersion(ctx context.Context, workID, versionID int64) (Work, error) {
	v, err := s.GetVersion(ctx, workID, versionID)
	if err != nil {
		return Work{}, err
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Work{}, fmt.Errorf("activate tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE works SET script = ?, active_version_id = ?, updated_at = ? WHERE id = ?`,
		v.Content.Script, versionID, db.NowUTC(), workID); err != nil {
		return Work{}, fmt.Errorf("activate update work: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Work{}, fmt.Errorf("activate commit: %w", err)
	}
	return s.GetWork(ctx, workID)
}
