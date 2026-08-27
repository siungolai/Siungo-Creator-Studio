package ai

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// fakeProvider 可控 Provider：依次返回预设结果/错误，记录调用次数与并发峰值。
type fakeProvider struct {
	mu          sync.Mutex
	results     []ChatResult
	errs        []error
	calls       int
	maxConcurrent int
	concurrent  int
	delay       time.Duration
}

func (f *fakeProvider) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	f.mu.Lock()
	f.concurrent++
	if f.concurrent > f.maxConcurrent {
		f.maxConcurrent = f.concurrent
	}
	i := f.calls
	f.calls++
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.concurrent--
		f.mu.Unlock()
	}()

	// 尊重 context：延迟期间超时则返回 ctx.Err()（验证超时覆盖路径）
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return ChatResult{}, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	var res ChatResult
	var err error
	if i < len(f.results) {
		res = f.results[i]
	}
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return res, err
}

func okResult(content string) ChatResult {
	return ChatResult{Content: content, Model: DefaultModel, PromptTokens: 10, CompletionTokens: 20}
}

const validJSON = `{"titles":["标题"],"script":"脚本内容","voiceover":"口播","tags":["t"],"cover_copy":"封面"}`

func testService(t *testing.T, p Provider, timeoutOverride int) (*Service, *AuditStore, *sql.DB) {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	audit := NewAuditStore(conn)
	var timeouts TimeoutReader
	if timeoutOverride > 0 {
		timeouts = &fakeTimeouts{v: timeoutOverride}
	}
	return NewService(p, audit, timeouts), audit, conn
}

type fakeTimeouts struct{ v int }

func (f *fakeTimeouts) AITimeoutSeconds(ctx context.Context) (int, error) { return f.v, nil }

func TestGenerateSuccessAndAudit(t *testing.T) {
	fake := &fakeProvider{results: []ChatResult{okResult(validJSON)}}
	svc, _, _ := testService(t, fake, 0)

	res, err := svc.Generate(context.Background(), "script", 0, "sys", "user prompt")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Output.Script != "脚本内容" || len(res.Output.Titles) != 1 {
		t.Fatalf("output wrong: %+v", res.Output)
	}
	if res.Model != DefaultModel {
		t.Fatalf("model = %q", res.Model)
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
}

func TestGenerateRetryOnParseFailure(t *testing.T) {
	// 第一次输出非 JSON，第二次成功 → 自动重试 1 次（G7）
	fake := &fakeProvider{
		results: []ChatResult{okResult("这不是 JSON"), okResult(validJSON)},
	}
	svc, _, _ := testService(t, fake, 0)
	res, err := svc.Generate(context.Background(), "script", 0, "sys", "p")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Output.Script != "脚本内容" {
		t.Fatalf("output wrong: %+v", res.Output)
	}
	if fake.calls != 2 {
		t.Fatalf("calls = %d, want 2（重试一次）", fake.calls)
	}
}

func TestGenerateBothFailAuditError(t *testing.T) {
	fake := &fakeProvider{
		results: []ChatResult{okResult("垃圾"), okResult("还是垃圾")},
	}
	svc, audit, _ := testService(t, fake, 0)
	_, err := svc.Generate(context.Background(), "script", 7, "sys", "p")
	if err == nil {
		t.Fatal("want error")
	}
	if fake.calls != 2 {
		t.Fatalf("calls = %d, want 2", fake.calls)
	}
	// 审计落库 status=error 且保留输出原文摘要
	rec, err := auditRecordByWorkID(t, audit, 7)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if rec.Status != "error" || rec.Kind != "script" || rec.WorkID != 7 {
		t.Fatalf("audit wrong: %+v", rec)
	}
	if !strings.Contains(rec.OutputSummary, "垃圾") {
		t.Fatalf("output summary should keep raw: %q", rec.OutputSummary)
	}
	if rec.Error == "" {
		t.Fatal("error should be recorded")
	}
}

func TestGenerateNilProvider(t *testing.T) {
	svc, audit, _ := testService(t, nil, 0)
	_, err := svc.Generate(context.Background(), "script", 0, "sys", "p")
	if err == nil || !strings.Contains(err.Error(), "AI_API_KEY") {
		t.Fatalf("want AI_API_KEY error, got %v", err)
	}
	// 无 key 也落审计（error）
	rec, err := auditRecordByWorkID(t, audit, 0)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if rec.Status != "error" {
		t.Fatalf("status = %q", rec.Status)
	}
}

func TestGenerateTimeoutOverride(t *testing.T) {
	// settings 覆盖超时 1s，provider 耗时 2s → 超时中止
	fake := &fakeProvider{results: []ChatResult{okResult(validJSON)}, delay: 2 * time.Second}
	svc, audit, _ := testService(t, fake, 1)
	_, err := svc.Generate(context.Background(), "script", 0, "sys", "p")
	if err == nil {
		t.Fatal("want timeout error")
	}
	rec, err := auditRecordByWorkID(t, audit, 0)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if rec.Status != "error" {
		t.Fatalf("status = %q, want error", rec.Status)
	}
}

func TestGenerateMutexSerial(t *testing.T) {
	// 并发两个 Generate → 串行执行（maxConcurrent = 1）
	fake := &fakeProvider{results: []ChatResult{okResult(validJSON), okResult(validJSON)}, delay: 100 * time.Millisecond}
	svc, _, _ := testService(t, fake, 0)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Generate(context.Background(), "script", 0, "sys", "p")
		}()
	}
	wg.Wait()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.maxConcurrent != 1 {
		t.Fatalf("maxConcurrent = %d, want 1（模块级互斥）", fake.maxConcurrent)
	}
	if fake.calls != 2 {
		t.Fatalf("calls = %d, want 2", fake.calls)
	}
}

// ---- helpers ----

func auditRecordByWorkID(t *testing.T, audit *AuditStore, workID int64) (AuditRecord, error) {
	t.Helper()
	conn := audit.conn
	var r AuditRecord
	var errText sql.NullString
	err := conn.QueryRow(`SELECT id, work_id, kind, provider, model, prompt_summary, output_summary,
		prompt_tokens, completion_tokens, duration_ms, status, error, created_at
		FROM ai_calls WHERE work_id = ? ORDER BY id DESC LIMIT 1`, workID).
		Scan(&r.ID, &r.WorkID, &r.Kind, &r.Provider, &r.Model, &r.PromptSummary, &r.OutputSummary,
			&r.PromptTokens, &r.CompletionTokens, &r.DurationMs, &r.Status, &errText, &r.CreatedAt)
	if err != nil {
		return AuditRecord{}, err
	}
	if errText.Valid {
		r.Error = errText.String
	}
	return r, nil
}
