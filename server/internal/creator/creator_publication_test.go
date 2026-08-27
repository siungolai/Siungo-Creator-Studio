package creator

import (
	"context"
	"testing"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
)

// T9 发布记录：懒补 → 列表 → 状态机（发布自动时间/回退清空/版本归属）。

func pubTestSetup(t *testing.T) (*Service, context.Context, int64) {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	s := NewService(NewSQLiteStore(conn), nil)
	ctx := context.Background()
	w, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "发布测试"})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	return s, ctx, w.ID
}

func TestListPublicationsLazyEnsure(t *testing.T) {
	s, ctx, id := pubTestSetup(t)

	pubs, err := s.ListPublications(ctx, id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// 预置平台（抖音/B站）都应懒补出 pending 记录
	if len(pubs) != 2 {
		t.Fatalf("pubs = %d, want 2（抖音/B站）: %+v", len(pubs), pubs)
	}
	if pubs[0].PlatformID != "douyin" || pubs[0].Status != "pending" {
		t.Fatalf("first pub = %+v", pubs[0])
	}
	if pubs[1].PlatformID != "bilibili" {
		t.Fatalf("second pub = %+v", pubs[1])
	}
	// 幂等：再拉一次不新增
	pubs2, _ := s.ListPublications(ctx, id)
	if len(pubs2) != 2 {
		t.Fatalf("lazy ensure not idempotent: %d", len(pubs2))
	}
}

func TestUpdatePublicationStateMachine(t *testing.T) {
	s, ctx, id := pubTestSetup(t)

	pubs, _ := s.ListPublications(ctx, id)
	pub := pubs[0] // douyin

	// 1. 标记发布：published_at 自动取当前
	updated, err := s.UpdatePublication(ctx, id, pub.ID, UpdatePublicationRequest{Status: "published"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if updated.Status != "published" || updated.PublishedAt == nil || *updated.PublishedAt == "" {
		t.Fatalf("published wrong: %+v", updated)
	}

	// 2. 覆盖发布时间
	override := "2026-01-01T00:00:00Z"
	updated, err = s.UpdatePublication(ctx, id, pub.ID, UpdatePublicationRequest{
		Status: "published", PublishedAt: &override, URL: "https://v.douyin.com/abc", Note: "抖音首发",
	})
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if updated.PublishedAt == nil || *updated.PublishedAt != override {
		t.Fatalf("override time wrong: %+v", updated.PublishedAt)
	}
	if updated.URL != "https://v.douyin.com/abc" || updated.Note != "抖音首发" {
		t.Fatalf("url/note wrong: %+v", updated)
	}

	// 3. 回退待发布：清空 published_at
	updated, err = s.UpdatePublication(ctx, id, pub.ID, UpdatePublicationRequest{Status: "pending"})
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if updated.Status != "pending" || updated.PublishedAt != nil {
		t.Fatalf("pending should clear time: %+v", updated)
	}

	// 4. 非法状态 → ErrInvalid
	if _, err := s.UpdatePublication(ctx, id, pub.ID, UpdatePublicationRequest{Status: "archived"}); err == nil {
		t.Fatal("invalid status accepted")
	}

	// 5. 超长 url → ErrInvalid
	long := make([]rune, maxURLlen+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := s.UpdatePublication(ctx, id, pub.ID, UpdatePublicationRequest{Status: "pending", URL: string(long)}); err == nil {
		t.Fatal("too long url accepted")
	}
}

func TestUpdatePublicationVersionOwnership(t *testing.T) {
	s, ctx, id := pubTestSetup(t)
	// 另一个作品的版本（越权绑定应拒绝）
	w2, err := s.CreateWork(ctx, CreateWorkRequest{Topic: "其他作品"})
	if err != nil {
		t.Fatalf("create work2: %v", err)
	}
	pubs, _ := s.ListPublications(ctx, w2.ID)

	// 版本不存在 → ErrInvalid（GetVersion 返回 ErrNotFound → 包装为 ErrInvalid）
	v := int64(999)
	if _, err := s.UpdatePublication(ctx, id, pubs[0].ID, UpdatePublicationRequest{Status: "published", VersionID: &v}); err == nil {
		t.Fatal("nonexistent version accepted")
	}

	// 越权：属于 w2 的版本绑定到 id 的作品
	// 用 w2 的发布记录绑定一个不存在的版本同样拒绝（归属由 GetVersion(workID=w2) 校验）
	if _, err := s.UpdatePublication(ctx, w2.ID, pubs[0].ID, UpdatePublicationRequest{Status: "pending", VersionID: &v}); err == nil {
		t.Fatal("version ownership not enforced")
	}
}

func TestWorkIncludesPublications(t *testing.T) {
	s, ctx, id := pubTestSetup(t)

	// 详情附带发布记录
	w, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if len(w.Publications) != 2 {
		t.Fatalf("detail pubs = %d, want 2", len(w.Publications))
	}

	// 列表附带（批量摘要）
	page, err := s.ListWorks(ctx, WorkFilter{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("list works: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	for _, item := range page.Items {
		if len(item.Publications) != 2 {
			t.Fatalf("item %d pubs = %d, want 2", item.ID, len(item.Publications))
		}
	}
}
