// HTTP 层：认证相关 handler 与中间件（业务逻辑见 auth.go，数据访问见 auth_store.go）。
package auth

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
)

type loginRequest struct {
	Password string `json:"password"`
}

// LoginHandler POST /api/v1/login：校验密码 → 惰性清理过期会话 → 创建持久会话 → Set-Cookie。
// 审计：成功/失败/冷却均记日志（含 IP 与 trace ID，不记密码）。
func (a *Auth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// 冷却检查先于密码校验（避免探测）
		ip := a.clientIP(r)
		if a.Blocked(ip) {
			log.Printf("auth: login blocked ip=%s trace=%s", ip, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusTooManyRequests, "too many attempts, try later")
			return
		}
		var req loginRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			a.RecordFailure(ip)
			log.Printf("auth: login invalid request ip=%s trace=%s", ip, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if !a.VerifyPassword(req.Password) {
			a.RecordFailure(ip)
			log.Printf("auth: login failed ip=%s trace=%s", ip, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		a.ClearFailures(ip)
		log.Printf("auth: login ok ip=%s trace=%s", ip, httpx.TraceID(r.Context()))

		// 登录时惰性清理过期会话（G17/S3：无定时任务）；失败仅记录不阻塞
		if err := a.CleanupExpired(r.Context()); err != nil {
			log.Printf("auth: lazy cleanup expired sessions: %v", err)
		}

		token, expiresAt, err := a.CreateSession(r.Context())
		if err != nil {
			log.Printf("auth: create session failed ip=%s: %v", ip, err)
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		http.SetCookie(w, a.sessionCookie(token, expiresAt))
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	})
}

// LogoutHandler POST /api/v1/logout：删除当前会话并清除 cookie。
func (a *Auth) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if tok := sessionToken(r); tok != "" {
			if err := a.DeleteSession(r.Context(), tok); err != nil {
				// 登出失败留痕（会话会随 30 天过期自然失效，但应可审计）
				log.Printf("auth: delete session on logout: %v", err)
			}
		}
		http.SetCookie(w, a.sessionCookie("", ""))
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
	})
}

// MeHandler GET /api/v1/me：会话校验（前端守卫与探测用）。
func (a *Auth) MeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !a.IsValidSession(r.Context(), sessionToken(r)) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	})
}

// Middleware 认证中间件：无效会话直接 401；有效则放行（后续里程碑的业务 API 挂载于此）。
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.IsValidSession(r.Context(), sessionToken(r)) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sessionCookie 构造会话 cookie；Secure 由部署配置决定（COOKIE_SECURE=true 时开启）。
// 登出场景传空 token/expiresAt 生成清除 cookie。
func (a *Auth) sessionCookie(token, expiresAt string) *http.Cookie {
	c := &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		c.MaxAge = -1
	} else if exp, err := time.Parse(time.RFC3339, expiresAt); err == nil {
		c.Expires = exp
	}
	if a.cookieSecure {
		c.Secure = true
	}
	return c
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// clientIP 取客户端 IP（冷却 key）。
// 默认不信任 X-Forwarded-For（可伪造）；nginx 反代部署且 TRUST_XFF=true 时优先取首项。
func (a *Auth) clientIP(r *http.Request) string {
	if a.trustXFF {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i >= 0 {
				xff = xff[:i]
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
