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

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// testSetup 建内存库（复用生产迁移与平台预置）+ store + service（默认无 AI，生成接口返回不可用）。
func testSetup(t *testing.T) *Service {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewService(NewSQLiteStore(conn), nil)
}

func TestCreateWorkValidation(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()

	// 空主题 → ErrInvalid
	if _, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty topic: want ErrInvalid, got %v", err)
	}
	// 主题超长 → ErrInvalid
	if _, err := s.CreateWork(ctx, CreateWorkRequest{Topic: strings.Repeat("长", 201)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long topic: want ErrInvalid, got %v", err)
	}
	// 标题超长 → ErrInvalid
	if _, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "ok", Title: strings.Repeat("题", 101)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long title: want ErrInvalid, got %v", err)
	}
	// 标题可空 + 主题 trim
	w, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "  短视频选题技巧  ", Title: " "})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Topic != "短视频选题技巧" || w.Title != "" {
		t.Fatalf("topic/title trim wrong: %q %q", w.Topic, w.Title)
	}
	if w.Status != "draft" || w.Style != "default" {
		t.Fatalf("defaults wrong: %q %q", w.Status, w.Style)
	}
	if w.Tags == nil {
		t.Fatal("tags should be empty slice, not nil")
	}
	if w.ID == 0 {
		t.Fatal("id should be assigned")
	}
}

func TestListWorksOrderAndEmpty(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()

	// 空列表 → 空切片非 nil
	page, err := s.ListWorks(ctx, WorkFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("empty list: got %#v", page.Items)
	}

	a, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "A"})
	b, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "B"})
	page, _ = s.ListWorks(ctx, WorkFilter{})
	if len(page.Items) != 2 || page.Total != 2 {
		t.Fatalf("len = %d total = %d, want 2/2", len(page.Items), page.Total)
	}
	// updated_at 倒序（同秒时 id 倒序兜底）：B 在前
	if page.Items[0].ID != b.ID || page.Items[1].ID != a.ID {
		t.Fatalf("order wrong: %d %d", page.Items[0].ID, page.Items[1].ID)
	}
}

func TestDeleteWork(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "删我"})

	// 删除不存在 → ErrNotFound
	if err := s.DeleteWork(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
	// 删除存在 → 成功且列表为空
	if err := s.DeleteWork(ctx, w.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	page, _ := s.ListWorks(ctx, WorkFilter{})
	if len(page.Items) != 0 {
		t.Fatalf("after delete len = %d, want 0", len(page.Items))
	}
}

func TestPlatformsSeeded(t *testing.T) {
	s := testSetup(t)
	platforms, err := s.ListPlatforms(context.Background())
	if err != nil {
		t.Fatalf("list platforms: %v", err)
	}
	if len(platforms) != 2 {
		t.Fatalf("len = %d, want 2 (douyin/bilibili)", len(platforms))
	}
	if platforms[0].ID != "douyin" || platforms[0].Name != "抖音" || !platforms[0].Enabled {
		t.Fatalf("platform[0] wrong: %+v", platforms[0])
	}
	if platforms[1].ID != "bilibili" {
		t.Fatalf("platform[1] wrong: %+v", platforms[1])
	}
	// 排序
	if !(platforms[0].Sort <= platforms[1].Sort) {
		t.Fatalf("platform sort order wrong: %d > %d", platforms[0].Sort, platforms[1].Sort)
	}
}

// ---- HTTP 层 ----

func httpRequest(method, path, body string) *http.Request {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHTTPCreateAndList(t *testing.T) {
	s := testSetup(t)
	// 空 topic → 400
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost, "/api/v1/creator/works", `{"topic":"  "}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty topic status = %d, want 400", rec.Code)
	}
	// 新建 → 201
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPost, "/api/v1/creator/works", `{"topic":"AI 口播脚本","title":"测试"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}
	var w Work
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if w.Topic != "AI 口播脚本" || w.Title != "测试" {
		t.Fatalf("work wrong: %+v", w)
	}
	// 列表包含（分页结构）
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var page WorkPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != w.ID || page.Total != 1 {
		t.Fatalf("list wrong: %+v", page)
	}
}

func TestHTTPDelete(t *testing.T) {
	s := testSetup(t)
	w, _ := s.CreateWork(context.Background(), CreateWorkRequest{Topic: "删"})

	// 非法 id → 400
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodDelete, "/api/v1/creator/works/abc", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status = %d, want 400", rec.Code)
	}
	// 不存在 → 404
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodDelete, "/api/v1/creator/works/999999", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
	// 存在 → 204
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodDelete, "/api/v1/creator/works/"+strconv.Itoa(int(w.ID)), ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}
}

func TestHTTPPlatforms(t *testing.T) {
	s := testSetup(t)
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/platforms", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("platforms status = %d", rec.Code)
	}
	var platforms []Platform
	if err := json.Unmarshal(rec.Body.Bytes(), &platforms); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(platforms) != 2 {
		t.Fatalf("len = %d, want 2", len(platforms))
	}
}

// ---- T4：详情与更新 ----

func TestUpdateWorkValidation(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "原主题"})

	base := UpdateWorkRequest{Topic: "新主题", Style: "ganhuo", Status: "making", Tags: []string{"a"}, Script: "正文"}

	// 空主题
	bad := base
	bad.Topic = "  "
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty topic: want ErrInvalid, got %v", err)
	}
	// 非法风格
	bad = base
	bad.Style = "unknown"
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad style: want ErrInvalid, got %v", err)
	}
	// 非法状态
	bad = base
	bad.Status = "deleted"
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad status: want ErrInvalid, got %v", err)
	}
	// 标签超限（11 个）
	bad = base
	bad.Tags = make([]string, 11)
	for i := range bad.Tags {
		bad.Tags[i] = "t"
	}
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many tags: want ErrInvalid, got %v", err)
	}
	// 标签为空项
	bad = base
	bad.Tags = []string{"ok", " "}
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty tag: want ErrInvalid, got %v", err)
	}
	// 脚本超长
	bad = base
	bad.Script = strings.Repeat("字", maxScriptLen+1)
	if _, err := s.UpdateWork(ctx, w.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long script: want ErrInvalid, got %v", err)
	}
	// 更新不存在 → ErrNotFound
	if _, err := s.UpdateWork(ctx, 999999, base); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: want ErrNotFound, got %v", err)
	}
}

func TestUpdateWorkPersistsAndArchiveNotLocked(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()
	w, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "原主题"})

	// 更新为归档
	updated, err := s.UpdateWork(ctx, w.ID, UpdateWorkRequest{
		Topic: "新主题", Title: "新标题", Style: "juqing", Status: "archived",
		Tags: []string{"标签一", "标签二"}, Script: "第一行\n第二行",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != "archived" || updated.Style != "juqing" || updated.Title != "新标题" {
		t.Fatalf("updated fields wrong: %+v", updated)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "标签一" {
		t.Fatalf("tags wrong: %+v", updated.Tags)
	}
	if updated.Script != "第一行\n第二行" {
		t.Fatalf("script wrong: %q", updated.Script)
	}
	// 持久化：重新读取
	got, err := s.GetWork(ctx, w.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Topic != "新主题" || got.Script != "第一行\n第二行" || got.Status != "archived" {
		t.Fatalf("persist wrong: %+v", got)
	}
	// updated_at 刷新
	if !(got.UpdatedAt >= w.UpdatedAt) {
		t.Fatalf("updated_at not refreshed: %s vs %s", got.UpdatedAt, w.UpdatedAt)
	}
	// 归档不锁操作：archived → draft 可切换（T4 验收）
	back, err := s.UpdateWork(ctx, w.ID, UpdateWorkRequest{
		Topic: "新主题", Style: "juqing", Status: "draft", Tags: []string{"标签一"}, Script: got.Script,
	})
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if back.Status != "draft" {
		t.Fatalf("status = %q, want draft", back.Status)
	}
}

func TestHTTPGetAndUpdate(t *testing.T) {
	s := testSetup(t)
	w, _ := s.CreateWork(context.Background(), CreateWorkRequest{Topic: "详情主题"})

	// GET 详情 → 200
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works/"+strconv.Itoa(int(w.ID)), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", rec.Code)
	}
	var got Work
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != w.ID || got.Topic != "详情主题" {
		t.Fatalf("get wrong: %+v", got)
	}

	// GET 不存在 → 404
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works/999999", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing status = %d, want 404", rec.Code)
	}

	// PUT 更新 → 200
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPut, "/api/v1/creator/works/"+strconv.Itoa(int(w.ID)),
		`{"topic":"改后主题","style":"tucao","status":"published","tags":["x"],"script":"副本内容"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200", rec.Code)
	}
	var updated Work
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Topic != "改后主题" || updated.Status != "published" || updated.Script != "副本内容" {
		t.Fatalf("put wrong: %+v", updated)
	}

	// PUT 非法状态 → 400
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPut, "/api/v1/creator/works/"+strconv.Itoa(int(w.ID)),
		`{"topic":"x","style":"default","status":"bad"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("put bad status = %d, want 400", rec.Code)
	}

	// PUT 不存在 → 404
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodPut, "/api/v1/creator/works/999999",
		`{"topic":"x","style":"default","status":"draft"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("put missing status = %d, want 404", rec.Code)
	}
}

// ---- T5：搜索 / 筛选 / 分页 ----

func TestListWorksFilterPaging(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()
	a, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "AI 口播脚本技巧", Title: "口播入门"})
	if _, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "美食探店"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, _ := s.CreateWork(ctx, CreateWorkRequest{Topic: "AI 绘画入门"})
	// c 改归档（制造不同状态）
	if _, err := s.UpdateWork(ctx, c.ID, UpdateWorkRequest{
		Topic: "AI 绘画入门", Style: "default", Status: "archived", Tags: []string{}, Script: "",
	}); err != nil {
		t.Fatalf("archive c: %v", err)
	}

	// 分页 limit=2 → 前 2 条（倒序：c, b）
	page, err := s.ListWorks(ctx, WorkFilter{Limit: 2})
	if err != nil {
		t.Fatalf("paging: %v", err)
	}
	if len(page.Items) != 2 || page.Total != 3 {
		t.Fatalf("limit=2: items=%d total=%d, want 2/3", len(page.Items), page.Total)
	}
	if page.Items[0].ID != c.ID {
		t.Fatalf("first item = %d, want %d (倒序)", page.Items[0].ID, c.ID)
	}
	// offset=2 → 最后一条
	page, _ = s.ListWorks(ctx, WorkFilter{Limit: 2, Offset: 2})
	if len(page.Items) != 1 || page.Items[0].ID != a.ID {
		t.Fatalf("offset=2: %+v", page.Items)
	}
	// offset 越界 → 空 items 但 total 正确
	page, _ = s.ListWorks(ctx, WorkFilter{Limit: 2, Offset: 10})
	if len(page.Items) != 0 || page.Total != 3 {
		t.Fatalf("offset=10: items=%d total=%d", len(page.Items), page.Total)
	}
	// 状态过滤
	page, _ = s.ListWorks(ctx, WorkFilter{Status: "archived"})
	if len(page.Items) != 1 || page.Items[0].ID != c.ID {
		t.Fatalf("status filter: %+v", page.Items)
	}
	// 关键词搜索（命中主题；标题"口播入门"也可命中 topic 无关）
	page, _ = s.ListWorks(ctx, WorkFilter{Query: "AI"})
	if len(page.Items) != 2 {
		t.Fatalf("q=AI: len=%d, want 2", len(page.Items))
	}
	// 搜索命中标题
	page, _ = s.ListWorks(ctx, WorkFilter{Query: "口播"})
	if len(page.Items) != 1 || page.Items[0].ID != a.ID {
		t.Fatalf("q=口播: %+v", page.Items)
	}
	// 搜索 + 状态组合
	page, _ = s.ListWorks(ctx, WorkFilter{Query: "AI", Status: "archived"})
	if len(page.Items) != 1 || page.Items[0].ID != c.ID {
		t.Fatalf("q+status: %+v", page.Items)
	}
	// 非法参数
	if _, err := s.ListWorks(ctx, WorkFilter{Offset: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("offset<0: want ErrInvalid, got %v", err)
	}
	if _, err := s.ListWorks(ctx, WorkFilter{Limit: 101}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit>100: want ErrInvalid, got %v", err)
	}
	if _, err := s.ListWorks(ctx, WorkFilter{Status: "bad"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad status: want ErrInvalid, got %v", err)
	}
}

func TestHTTPListFilterPaging(t *testing.T) {
	s := testSetup(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "主题" + strconv.Itoa(i)}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// limit=2 分页结构
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works?limit=2", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var page WorkPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 2 || page.Total != 3 || page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("page wrong: %+v", page)
	}

	// 非法 limit → 400
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works?limit=abc", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d, want 400", rec.Code)
	}
	// 非法 status → 400
	rec = httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httpRequest(http.MethodGet, "/api/v1/creator/works?status=bad", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status status = %d, want 400", rec.Code)
	}
}
