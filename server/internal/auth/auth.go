// Package auth 提供单密码认证业务：bcrypt 校验、持久化会话（30 天，G17）、登录冷却（5 次/30 秒）。
// HTTP 层（handler/中间件）见 auth_http.go；数据访问见 auth_store.go；本文件仅业务逻辑。
// key/密码仅来自环境变量，不入库、不进设置界面、不落日志。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// SessionCookie 会话 cookie 名
	SessionCookie = "session"
	// SessionTTL 会话有效期（PRD G17：30 天）
	SessionTTL = 30 * 24 * time.Hour
	// MaxFailures 冷却阈值（PRD：连续 5 次失败）
	MaxFailures = 5
)

// Store 会话存储抽象（SQLite 实现见 auth_store.go；接口便于单测替换）。
type Store interface {
	CreateSession(ctx context.Context, tokenHash, expiresAt string) error
	GetSession(ctx context.Context, tokenHash string) (expiresAt string, err error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpired(ctx context.Context, now string) error
}

// Options 认证服务配置。
type Options struct {
	FailCooldown time.Duration // 登录失败冷却窗口（0 时用默认 30s）
	CookieSecure bool          // 会话 cookie 是否带 Secure（https 部署时开启）
	TrustXFF     bool          // 是否信任 X-Forwarded-For（nginx 反代部署时开启；默认不信任防伪造）
}

// Auth 认证服务（业务层，无 HTTP 依赖）。
type Auth struct {
	store        Store
	passwordBc   []byte // 启动时生成的 bcrypt hash；内存中不保留明文比较路径
	failCooldown time.Duration
	cookieSecure bool
	trustXFF     bool
	mu           sync.Mutex
	failures     map[string]*failCounter // key: 客户端 IP
}

type failCounter struct {
	count int
	until time.Time // 冷却解除时间
}

// New 创建认证服务；password 为空返回错误（由 main 启动时拒绝）。
func New(store Store, password string, opts Options) (*Auth, error) {
	if password == "" {
		return nil, errors.New("auth: password is empty")
	}
	bc, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}
	cooldown := opts.FailCooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &Auth{
		store:        store,
		passwordBc:   bc,
		failCooldown: cooldown,
		cookieSecure: opts.CookieSecure,
		trustXFF:     opts.TrustXFF,
		failures:     make(map[string]*failCounter),
	}, nil
}

// VerifyPassword 校验密码（bcrypt 恒定时间比较）。
func (a *Auth) VerifyPassword(password string) bool {
	return bcrypt.CompareHashAndPassword(a.passwordBc, []byte(password)) == nil
}

// IsValidSession 校验会话令牌是否有效（存在且未过期）。
func (a *Auth) IsValidSession(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	expiresAt, err := a.store.GetSession(ctx, hashToken(token))
	if err != nil {
		return false
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(exp)
}

// CreateSession 创建会话并返回令牌与过期时间（登录成功调用）。
func (a *Auth) CreateSession(ctx context.Context) (token, expiresAt string, err error) {
	token, err = newToken()
	if err != nil {
		return "", "", err
	}
	expiresAt = time.Now().UTC().Add(SessionTTL).Format(time.RFC3339)
	if err := a.store.CreateSession(ctx, hashToken(token), expiresAt); err != nil {
		return "", "", err
	}
	return token, expiresAt, nil
}

// DeleteSession 删除指定令牌的会话（登出调用）。
func (a *Auth) DeleteSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return a.store.DeleteSession(ctx, hashToken(token))
}

// CleanupExpired 惰性清理过期会话（登录时调用；失败仅记录，不阻塞登录）。
func (a *Auth) CleanupExpired(ctx context.Context) error {
	return a.store.DeleteExpired(ctx, dbNow())
}

// ---- 冷却 ----

// Blocked 是否处于冷却封锁（连续失败达阈值且未过窗口）。
func (a *Auth) Blocked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.failures[ip]
	if !ok || time.Now().After(f.until) {
		return false
	}
	return f.count >= MaxFailures
}

// RecordFailure 记录一次登录失败（刷新冷却窗口）。
func (a *Auth) RecordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	f, ok := a.failures[ip]
	if !ok || now.After(f.until) {
		a.failures[ip] = &failCounter{count: 1, until: now.Add(a.failCooldown)}
		return
	}
	f.count++
	f.until = now.Add(a.failCooldown)
}

// ClearFailures 登录成功后清零该 IP 的失败计数。
func (a *Auth) ClearFailures(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, ip)
}

// ---- helpers ----

// newToken 生成 32 字节随机会话令牌（hex 编码后下发 cookie）。
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken 令牌只以 SHA-256 落库（防 DB 泄露直接劫持会话）。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func dbNow() string { return time.Now().UTC().Format(time.RFC3339) }
