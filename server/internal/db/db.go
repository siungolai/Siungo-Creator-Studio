// Package db 提供 SQLite 打开与轻量迁移（CREATE TABLE IF NOT EXISTS，随里程碑按需追加）。
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
// path 为空时使用默认相对路径 "data.db"；":memory:" 供测试使用。
func Open(path string) (*sql.DB, error) {
	if path == "" {
		path = "data.db"
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// 单用户工具：单连接足够，且规避 SQLite 写锁竞争
	conn.SetMaxOpenConns(1)
	// 启用外键约束（works 级联删除版本/发布记录依赖此开关）
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// migrate 幂等迁移。新增表/列在此追加，不引入迁移框架。
func migrate(conn *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			sort INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS works (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL DEFAULT '',
			topic TEXT NOT NULL DEFAULT '',
			style TEXT NOT NULL DEFAULT 'default',
			script TEXT NOT NULL DEFAULT '',
			active_version_id INTEGER,
			tags TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'draft',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_id INTEGER,
			kind TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			prompt_summary TEXT NOT NULL DEFAULT '',
			output_summary TEXT NOT NULL DEFAULT '',
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			error TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS work_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
			platform TEXT NOT NULL DEFAULT '',
			content_json TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS work_publications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
			platform_id TEXT NOT NULL REFERENCES platforms(id),
			version_id INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			url TEXT NOT NULL DEFAULT '',
			published_at TEXT,
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(work_id, platform_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// 平台基础数据预置（表驱动扩展：改数据即加平台；T3 验收项）
	seeds := []string{
		`INSERT OR IGNORE INTO platforms (id, name, sort, enabled) VALUES ('douyin', '抖音', 1, 1)`,
		`INSERT OR IGNORE INTO platforms (id, name, sort, enabled) VALUES ('bilibili', 'B站', 2, 1)`,
	}
	for _, s := range seeds {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	return nil
}

// NowUTC 返回 UTC RFC3339 时间字符串（全库统一时间格式）。
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
