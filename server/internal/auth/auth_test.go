package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// testSetup 建内存库（复用生产迁移）+ store + auth，cooldown 可配短以加速测试。
func testSetup(t *testing.T, cooldown time.Duration) (*Auth, *SQLiteStore) {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	store := NewSQLiteStore(conn)
	a, err := New(store, "secret", Options{FailCooldown: cooldown})
	if err != nil {
		t.Fatalf("new auth: %v", err)
	}
	return a, store
}

// login 走登录接口并返回响应与 session cookie。
func login(t *testing.T, h http.Handler, password string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	body := strings.NewReader(`{"password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			cookie = c.Value
		}
	}
	return rec, cookie
}

// authedReq 构造带（或不带）session cookie 的请求。
func authedReq(method, path, cookie string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	}
	return req
}

func TestNewRejectsEmptyPassword(t *testing.T) {
	if _, err := New(nil, "", Options{FailCooldown: time.Second}); err == nil {
		t.Fatal("want error for empty password")
	}
}

func TestLoginSuccessAndMe(t *testing.T) {
	a, _ := testSetup(t, time.Second)
	rec, cookie := login(t, a.LoginHandler(), "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", rec.Code)
	}
	if cookie == "" {
		t.Fatal("no session cookie set")
	}
	// 带 cookie 访问 /api/me → 200
	me := httptest.NewRecorder()
	a.MeHandler().ServeHTTP(me, authedReq(http.MethodGet, "/api/me", cookie))
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200", me.Code)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	a, _ := testSetup(t, time.Second)
	rec, _ := login(t, a.LoginHandler(), "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginFailureCooldown(t *testing.T) {
	a, _ := testSetup(t, 100*time.Millisecond)
	// 前 5 次失败 → 401
	for i := 0; i < MaxFailures; i++ {
		rec, _ := login(t, a.LoginHandler(), "wrong")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	// 第 6 次 → 429（冷却触发）
	rec, _ := login(t, a.LoginHandler(), "wrong")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	// 冷却结束后恢复
	time.Sleep(150 * time.Millisecond)
	rec, _ = login(t, a.LoginHandler(), "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after cooldown status = %d, want 401", rec.Code)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	a, _ := testSetup(t, time.Second)
	_, cookie := login(t, a.LoginHandler(), "secret")
	if cookie == "" {
		t.Fatal("no session cookie")
	}
	// 登出
	out := httptest.NewRecorder()
	a.LogoutHandler().ServeHTTP(out, authedReq(http.MethodPost, "/api/logout", cookie))
	if out.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", out.Code)
	}
	// 旧 cookie 失效
	me := httptest.NewRecorder()
	a.MeHandler().ServeHTTP(me, authedReq(http.MethodGet, "/api/me", cookie))
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", me.Code)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	a, store := testSetup(t, time.Second)
	// 直接插入过期会话（模拟 30 天前创建的会话）
	ctx := context.Background()
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := store.CreateSession(ctx, hashToken("stale-token"), expired); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	me := httptest.NewRecorder()
	a.MeHandler().ServeHTTP(me, authedReq(http.MethodGet, "/api/me", "stale-token"))
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me with expired session = %d, want 401", me.Code)
	}
}

func TestDeleteExpiredOnly(t *testing.T) {
	_, store := testSetup(t, time.Second)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := store.CreateSession(ctx, "hash-expired", past); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.CreateSession(ctx, "hash-live", future); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.DeleteExpired(ctx, db.NowUTC()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := store.GetSession(ctx, "hash-expired"); err == nil {
		t.Fatal("expired session should be deleted")
	}
	if _, err := store.GetSession(ctx, "hash-live"); err != nil {
		t.Fatal("live session should remain")
	}
}

func TestMiddleware(t *testing.T) {
	a, _ := testSetup(t, time.Second)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := a.Middleware(next)

	// 无 cookie → 401 且 next 不执行
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedReq(http.MethodGet, "/api/x", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("next should not run without session")
	}
	// 有效 cookie → 放行
	_, cookie := login(t, a.LoginHandler(), "secret")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, authedReq(http.MethodGet, "/api/x", cookie))
	if rec2.Code != http.StatusOK {
		t.Fatalf("with cookie status = %d, want 200", rec2.Code)
	}
	if !called {
		t.Fatal("next should run with valid session")
	}
}
