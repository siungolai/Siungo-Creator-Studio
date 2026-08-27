// Package settings 提供模块设置存储与 API（PRD §4.3：settings 表 key/value；首版仅 AI 超时）。
// 敏感项（key 等）不在此存储——仅环境变量。
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// AI 超时设置项（PRD G5：30–600 秒，默认 180）。
const (
	KeyAITimeoutSeconds = "ai_timeout_seconds"
	DefaultTimeout      = 180
	MinTimeout          = 30
	MaxTimeout          = 600
)

// 生成提示词设置项（Issue 18：可调生成提示词；空串 = 恢复默认，存储层删除该 key）。
// 默认值（generateSystemPrompt / "主题：{topic}"）由 creator 包持有，settings 只存覆盖值。
const (
	KeyGenerateSystemPrompt        = "generate_system_prompt"
	KeyGenerateUserPromptTemplate  = "generate_user_prompt_template"
	MaxPromptLen                   = 5000 // 提示词长度上限（单 key；防超长滥用）
)

// ErrInvalid 设置值非法（HTTP 400 语义）。
var ErrInvalid = errors.New("settings: invalid value")

// Store settings 表数据访问。
type Store struct {
	conn *sql.DB
}

// NewStore 用已迁移的数据库连接构建设置存储。
func NewStore(conn *sql.DB) *Store {
	return &Store{conn: conn}
}

// GetInt 读取整数设置；不存在返回 (0, false, nil)。
func (s *Store) GetInt(ctx context.Context, key string) (int, bool, error) {
	var v string
	err := s.conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("settings get %s: %w", key, err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false, fmt.Errorf("settings get %s: parse %q: %w", key, v, err)
	}
	return n, true, nil
}

// SetInt 写入整数设置（UPSERT）。
func (s *Store) SetInt(ctx context.Context, key string, v int) error {
	if _, err := s.conn.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strconv.Itoa(v)); err != nil {
		return fmt.Errorf("settings set %s: %w", key, err)
	}
	return nil
}

// GetString 读取文本设置；不存在返回 ("", false, nil)。
func (s *Store) GetString(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("settings get %s: %w", key, err)
	}
	return v, true, nil
}

// SetString 写入文本设置（UPSERT）；空值表示删除该 key（恢复默认语义）。
func (s *Store) SetString(ctx context.Context, key, v string) error {
	if v == "" {
		if _, err := s.conn.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("settings delete %s: %w", key, err)
		}
		return nil
	}
	if _, err := s.conn.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, v); err != nil {
		return fmt.Errorf("settings set %s: %w", key, err)
	}
	return nil
}

// Service 设置业务（校验 + 默认值）。
type Service struct {
	store *Store
}

// NewService 构建设置服务。
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// AITimeoutSeconds 读取 AI 超时（秒）；未设置或读取出错时返回默认 180（PRD §4.5）。
func (s *Service) AITimeoutSeconds(ctx context.Context) (int, error) {
	v, ok, err := s.store.GetInt(ctx, KeyAITimeoutSeconds)
	if err != nil {
		return DefaultTimeout, err
	}
	if !ok {
		return DefaultTimeout, nil
	}
	if v < MinTimeout || v > MaxTimeout {
		return DefaultTimeout, nil // 存量脏值回退默认
	}
	return v, nil
}

// SetAITimeoutSeconds 校验（30–600）并写入 AI 超时。
func (s *Service) SetAITimeoutSeconds(ctx context.Context, v int) error {
	if v < MinTimeout || v > MaxTimeout {
		return fmt.Errorf("%w: ai_timeout_seconds must be %d-%d", ErrInvalid, MinTimeout, MaxTimeout)
	}
	return s.store.SetInt(ctx, KeyAITimeoutSeconds, v)
}

// GenerateSystemPrompt 读取生成系统提示词覆盖值；未设置返回 ("", nil)，调用方（creator）回退内置默认。
func (s *Service) GenerateSystemPrompt(ctx context.Context) (string, error) {
	v, ok, err := s.store.GetString(ctx, KeyGenerateSystemPrompt)
	if err != nil || !ok {
		return "", err
	}
	return v, nil
}

// GenerateUserPromptTemplate 读取生成用户提示模板覆盖值；未设置返回 ("", nil)。
func (s *Service) GenerateUserPromptTemplate(ctx context.Context) (string, error) {
	v, ok, err := s.store.GetString(ctx, KeyGenerateUserPromptTemplate)
	if err != nil || !ok {
		return "", err
	}
	return v, nil
}

// SetGenerateSystemPrompt 校验（长度上限；空串 = 恢复默认）并写入系统提示词。
func (s *Service) SetGenerateSystemPrompt(ctx context.Context, v string) error {
	if len([]rune(v)) > MaxPromptLen {
		return fmt.Errorf("%w: generate_system_prompt too long (max %d)", ErrInvalid, MaxPromptLen)
	}
	return s.store.SetString(ctx, KeyGenerateSystemPrompt, v)
}

// SetGenerateUserPromptTemplate 校验（长度上限；空串 = 恢复默认）并写入用户提示模板。
func (s *Service) SetGenerateUserPromptTemplate(ctx context.Context, v string) error {
	if len([]rune(v)) > MaxPromptLen {
		return fmt.Errorf("%w: generate_user_prompt_template too long (max %d)", ErrInvalid, MaxPromptLen)
	}
	return s.store.SetString(ctx, KeyGenerateUserPromptTemplate, v)
}
