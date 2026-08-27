package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

func testSetup(t *testing.T) *Service {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewService(NewStore(conn))
}

func TestAITimeoutDefault(t *testing.T) {
	s := testSetup(t)
	v, err := s.AITimeoutSeconds(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != DefaultTimeout {
		t.Fatalf("default = %d, want %d", v, DefaultTimeout)
	}
}

func TestAITimeoutSetAndPersist(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()

	// 越界拒绝
	if err := s.SetAITimeoutSeconds(ctx, MinTimeout-1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("min-1: want ErrInvalid, got %v", err)
	}
	if err := s.SetAITimeoutSeconds(ctx, MaxTimeout+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("max+1: want ErrInvalid, got %v", err)
	}
	// 合法写入并回读
	if err := s.SetAITimeoutSeconds(ctx, 240); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := s.AITimeoutSeconds(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != 240 {
		t.Fatalf("got %d, want 240", v)
	}
	// 覆盖
	if err := s.SetAITimeoutSeconds(ctx, DefaultTimeout); err != nil {
		t.Fatalf("reset: %v", err)
	}
	v, _ = s.AITimeoutSeconds(ctx)
	if v != DefaultTimeout {
		t.Fatalf("reset got %d, want %d", v, DefaultTimeout)
	}
}

func TestHTTPGetPut(t *testing.T) {
	s := testSetup(t)
	h := s.Handler()

	// GET 默认 180
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/creator/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	var resp settingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AITimeoutSeconds != DefaultTimeout {
		t.Fatalf("default = %d", resp.AITimeoutSeconds)
	}

	// PUT 越界 → 400
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/creator/settings",
		strings.NewReader(`{"aiTimeoutSeconds":10}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("put 10 status = %d, want 400", rec.Code)
	}

	// PUT 合法 → 200，GET 持久化
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/creator/settings",
		strings.NewReader(`{"aiTimeoutSeconds":200}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put 200 status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/creator/settings", nil))
	var got settingsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.AITimeoutSeconds != 200 {
		t.Fatalf("persisted = %d, want 200", got.AITimeoutSeconds)
	}
}

// Issue 18：提示词覆盖读写。
func TestPromptOverrides(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()

	// 初始未设置 → ("", nil)
	if v, err := s.GenerateSystemPrompt(ctx); err != nil || v != "" {
		t.Fatalf("sys initial = %q, %v", v, err)
	}
	if v, err := s.GenerateUserPromptTemplate(ctx); err != nil || v != "" {
		t.Fatalf("tpl initial = %q, %v", v, err)
	}

	// 写入并回读
	sys := "你是小红书风格助手。{topic} 主题下输出短文案。"
	if err := s.SetGenerateSystemPrompt(ctx, sys); err != nil {
		t.Fatalf("set sys: %v", err)
	}
	v, err := s.GenerateSystemPrompt(ctx)
	if err != nil || v != sys {
		t.Fatalf("sys got = %q, %v", v, err)
	}
	if err := s.SetGenerateUserPromptTemplate(ctx, "请为 {topic} 生成脚本（{platform}）"); err != nil {
		t.Fatalf("set tpl: %v", err)
	}
	v, _ = s.GenerateUserPromptTemplate(ctx)
	if v != "请为 {topic} 生成脚本（{platform}）" {
		t.Fatalf("tpl got = %q", v)
	}

	// 超长拒绝（rune 计数）
	long := strings.Repeat("长", MaxPromptLen+1)
	if err := s.SetGenerateSystemPrompt(ctx, long); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too long: want ErrInvalid, got %v", err)
	}

	// 空串 = 恢复默认（删除 key）
	if err := s.SetGenerateSystemPrompt(ctx, ""); err != nil {
		t.Fatalf("clear sys: %v", err)
	}
	if v, err := s.GenerateSystemPrompt(ctx); err != nil || v != "" {
		t.Fatalf("sys after clear = %q, %v", v, err)
	}
}

// Issue 18：PUT 部分更新兼容——只传超时不清提示词；空串显式清除。
func TestHTTPPutPartialUpdate(t *testing.T) {
	s := testSetup(t)
	h := s.Handler()
	ctx := context.Background()

	// 先写入提示词
	if err := s.SetGenerateSystemPrompt(ctx, "自定义系统提示"); err != nil {
		t.Fatalf("set sys: %v", err)
	}

	// 只 PUT 超时 → 提示词保持
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/creator/settings",
		strings.NewReader(`{"aiTimeoutSeconds":210}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d", rec.Code)
	}
	if v, _ := s.GenerateSystemPrompt(ctx); v != "自定义系统提示" {
		t.Fatalf("sys clobbered = %q", v)
	}

	// PUT 提示词与模板 → 生效
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/creator/settings",
		strings.NewReader(`{"generateSystemPrompt":"新提示","generateUserPromptTemplate":"主题 {topic}"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put prompts status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp settingsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.GenerateSystemPrompt != "新提示" || resp.GenerateUserPromptTemplate != "主题 {topic}" {
		t.Fatalf("resp prompts = %q / %q", resp.GenerateSystemPrompt, resp.GenerateUserPromptTemplate)
	}

	// 空串清除（恢复默认）
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/creator/settings",
		strings.NewReader(`{"generateSystemPrompt":""}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	if v, _ := s.GenerateSystemPrompt(ctx); v != "" {
		t.Fatalf("sys not cleared = %q", v)
	}
}
