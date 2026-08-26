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
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// NowUTC 返回 UTC RFC3339 时间字符串（全库统一时间格式）。
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
