package creator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/ai"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// fakeAI 可控生成能力。
type fakeAI struct {
	result ai.GenerateResult
	err    error
	calls  int
	lastSys string
	lastUser string
}

func (f *fakeAI) Generate(_ context.Context, _ string, _ int64, sys, user string) (ai.GenerateResult, error) {
	f.calls++
	f.lastSys = sys
	f.lastUser = user
	return f.result, f.err
}

func fakeResult() ai.GenerateResult {
	return ai.GenerateResult{
		Output: ai.GenerateOutput{
			Titles:    []string{"候选标题A", "候选标题B"},
			Script:    "钩子…主体…CTA",
			Voiceover: "口播稿",
			Tags:      []string{"AI"},
			CoverCopy: "封面",
		},
		Model: ai.DefaultModel,
	}
}

func testSetupWithAI(t *testing.T, f *fakeAI) *Service {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewService(NewSQLiteStore(conn), f)
}

func TestGenerateVersionSuccess(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "AI 短视频"})

	v, work, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{Platform: "douyin"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// 版本内容与平台维度
	if v.Platform != "douyin" || v.Model != ai.DefaultModel || v.WorkID != w.ID {
		t.Fatalf("version wrong: %+v", v)
	}
	if v.Content.Script != "钩子…主体…CTA" || len(v.Content.Titles) != 2 {
		t.Fatalf("version content wrong: %+v", v.Content)
	}
	// 生成即激活：副本刷新 + active_version_id + 标题回填 + 置 making
	if work.Script != "钩子…主体…CTA" {
		t.Fatalf("work script not refreshed: %q", work.Script)
	}
	if work.ActiveVersionID == nil || *work.ActiveVersionID != v.ID {
		t.Fatalf("active version = %v, want %d", work.ActiveVersionID, v.ID)
	}
	if work.Title != "候选标题A" {
		t.Fatalf("title not backfilled: %q", work.Title)
	}
	if work.Status != "making" {
		t.Fatalf("status = %q, want making", work.Status)
	}
	// 提示词含风格与平台微调
	if fake.lastUser != "主题：AI 短视频" {
		t.Fatalf("user prompt wrong: %q", fake.lastUser)
	}
	if !strings.Contains(fake.lastSys, "抖音") {
		t.Fatalf("system prompt should contain platform hint: %q", fake.lastSys)
	}
}

func TestGenerateVersionTitleKept(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "t", Title: "已有标题"})

	_, work, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if work.Title != "已有标题" {
		t.Fatalf("title overwritten: %q", work.Title)
	}
}

func TestGenerateVersionStyleAndTopicOverride(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "原主题"})

	if _, _, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{Style: "tucao", Topic: "新主题"}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if fake.lastUser != "主题：新主题" {
		t.Fatalf("user prompt = %q", fake.lastUser)
	}
	if !strings.Contains(fake.lastSys, "吐槽") {
		t.Fatalf("style hint missing: %q", fake.lastSys)
	}
}

func TestGenerateVersionValidation(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "t"})

	// 非法平台
	if _, _, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{Platform: "xhs"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad platform: want ErrInvalid, got %v", err)
	}
	// 非法风格
	if _, _, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{Style: "bad"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad style: want ErrInvalid, got %v", err)
	}
	// 作品不存在
	if _, _, err := s.GenerateVersion(ctx, 999999, GenerateRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing work: want ErrNotFound, got %v", err)
	}
}

func TestGenerateVersionAIFailureNoVersion(t *testing.T) {
	fake := &fakeAI{err: errors.New("upstream down")}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "t"})

	_, _, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{})
	if err == nil {
		t.Fatal("want error")
	}
	// 不产生版本 + 作品状态不变
	versions, _ := s.ListVersions(ctx, w.ID)
	if len(versions) != 0 {
		t.Fatalf("versions = %d, want 0", len(versions))
	}
	got, _ := s.GetWork(ctx, w.ID)
	if got.Status != "draft" || got.Script != "" {
		t.Fatalf("work changed: %+v", got)
	}
}

func TestGenerateVersionNilAI(t *testing.T) {
	s := testSetup(t) // 无 AI
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "t"})
	if _, _, err := s.GenerateVersion(ctx, w.ID, GenerateRequest{}); !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("want ErrAIUnavailable, got %v", err)
	}
}

func TestActivateVersion(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "t"})

	// 两个版本（第二个修改脚本内容）
	v1, _, _ := s.GenerateVersion(ctx, w.ID, GenerateRequest{})
	fake.result.Output.Script = "第二版脚本"
	v2, _, _ := s.GenerateVersion(ctx, w.ID, GenerateRequest{Platform: "bilibili"})

	// 版本列表倒序：v2 在前
	versions, _ := s.ListVersions(ctx, w.ID)
	if len(versions) != 2 || versions[0].ID != v2.ID {
		t.Fatalf("versions order wrong: %+v", versions)
	}

	// 激活 v1 → 副本刷新为 v1 脚本
	work, err := s.ActivateVersion(ctx, w.ID, v1.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if work.Script != "钩子…主体…CTA" {
		t.Fatalf("script = %q, want v1 script", work.Script)
	}
	if work.ActiveVersionID == nil || *work.ActiveVersionID != v1.ID {
		t.Fatalf("active = %v, want %d", work.ActiveVersionID, v1.ID)
	}
	// 版本不可变：v1 内容未被修改
	got, _ := s.GetVersion(ctx, w.ID, v1.ID)
	if got.Content.Script != "钩子…主体…CTA" {
		t.Fatalf("version mutated: %q", got.Content.Script)
	}

	// 跨作品访问版本 → ErrNotFound
	other, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "other"})
	if _, err := s.ActivateVersion(ctx, other.ID, v1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-work activate: want ErrNotFound, got %v", err)
	}
}

func TestHTTPGenerateAndVersions(t *testing.T) {
	fake := &fakeAI{result: fakeResult()}
	s := testSetupWithAI(t, fake)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "HTTP 生成"})
	idStr := strconv.Itoa(int(w.ID))

	// POST versions → 201（含版本与作品）
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost,
		"/api/v1/creator/works/"+idStr+"/versions", `{"platform":"douyin"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("post versions status = %d, want 201", rec.Code)
	}
	var body struct {
		Version Version `json:"version"`
		Work    Work    `json:"work"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version.Platform != "douyin" || body.Work.Status != "making" {
		t.Fatalf("body wrong: %+v", body)
	}

	// 非法平台 → 400
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost,
		"/api/v1/creator/works/"+idStr+"/versions", `{"platform":"bad"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad platform status = %d, want 400", rec.Code)
	}

	// 作品不存在 → 404
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost,
		"/api/v1/creator/works/999999/versions", `{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing work status = %d, want 404", rec.Code)
	}

	// GET versions → 200 且 1 条
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet,
		"/api/v1/creator/works/"+idStr+"/versions", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("get versions status = %d", rec.Code)
	}
	var versions []Version
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}

	// PUT activate → 200
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPut,
		"/api/v1/creator/works/"+idStr+"/versions/"+strconv.Itoa(int(versions[0].ID))+"/activate", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d, want 200", rec.Code)
	}

	// 无 AI 时 POST versions → 503
	s2 := testSetup(t)
	w2, _ := s2.CreateWork(ctx, CreateWorkRequest{Topic: "noai"})
	rec = httptest.NewRecorder()
	s2.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost,
		"/api/v1/creator/works/"+strconv.Itoa(int(w2.ID))+"/versions", `{}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no-ai status = %d, want 503", rec.Code)
	}
}
