package auth

import (
	"context"
	"database/sql"
)

// SQLiteStore 基于 database/sql 的会话存储（sessions 表由 db 包迁移创建）。
type SQLiteStore struct {
	conn *sql.DB
}

// NewSQLiteStore 用已迁移的数据库连接构建会话存储。
func NewSQLiteStore(conn *sql.DB) *SQLiteStore {
	return &SQLiteStore{conn: conn}
}

func (s *SQLiteStore) CreateSession(ctx context.Context, tokenHash, expiresAt string) error {
	_, err := s.conn.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		tokenHash, dbNow(), expiresAt)
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, tokenHash string) (string, error) {
	var expiresAt string
	err := s.conn.QueryRowContext(ctx,
		`SELECT expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&expiresAt)
	return expiresAt, err
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *SQLiteStore) DeleteExpired(ctx context.Context, now string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	return err
}
