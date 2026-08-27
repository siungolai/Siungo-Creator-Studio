package ai

import (
	"context"
	"database/sql"
	"fmt"
)

// AuditRecord AI 调用审计记录（ai_calls 表）。
type AuditRecord struct {
	ID               int64
	WorkID           int64
	Kind             string // script | topic（T9）
	Provider         string
	Model            string
	PromptSummary    string
	OutputSummary    string
	PromptTokens     int
	CompletionTokens int
	DurationMs       int64
	Status           string // ok | error
	Error            string
	CreatedAt        string
}

// AuditStore ai_calls 表数据访问。
type AuditStore struct {
	conn *sql.DB
}

// NewAuditStore 用已迁移的数据库连接构建审计存储。
func NewAuditStore(conn *sql.DB) *AuditStore {
	return &AuditStore{conn: conn}
}

// Insert 落库一次调用审计（成功与失败均记录，PRD §4.5）。
func (s *AuditStore) Insert(ctx context.Context, r AuditRecord) (int64, error) {
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO ai_calls
		 (work_id, kind, provider, model, prompt_summary, output_summary,
		  prompt_tokens, completion_tokens, duration_ms, status, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.WorkID, r.Kind, r.Provider, r.Model, r.PromptSummary, r.OutputSummary,
		r.PromptTokens, r.CompletionTokens, r.DurationMs, r.Status, nullStr(r.Error), r.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("audit insert: %w", err)
	}
	return res.LastInsertId()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
