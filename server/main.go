// Siungo Creator Studio — 自媒体 AI 制作工作台。
// 单二进制部署：go:embed 前端构建产物（server/static），/api/v1/* 为 JSON API，其余路径 SPA 回退。
// 路由分区：公开（health/login/logout/me）与受保护（/api/v1/ 其余，认证中间件；后续里程碑挂载于此）。
// 中间件链：Trace（trace ID）→ Recover（panic 兜底）→ 业务路由；优雅停机。
package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/auth"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/creator"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/health"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/web"
)

//go:embed static
var staticFS embed.FS

func main() {
	cfg := LoadConfig()
	if cfg.Password == "" {
		log.Fatal("STUDIO_PASSWORD 未设置，拒绝启动（敏感项一律环境变量）")
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	authSvc, err := auth.New(auth.NewSQLiteStore(conn), cfg.Password, auth.Options{
		FailCooldown: cfg.AuthFailCooldown,
		CookieSecure: cfg.CookieSecure,
		TrustXFF:     cfg.TrustXFF,
	})
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	creatorSvc := creator.NewService(creator.NewSQLiteStore(conn))

	mux := http.NewServeMux()
	mux.Handle("/api/v1/health", health.Handler())
	mux.Handle("/api/v1/login", authSvc.LoginHandler())
	mux.Handle("/api/v1/logout", authSvc.LogoutHandler())
	mux.Handle("/api/v1/me", authSvc.MeHandler())

	// 受保护 API 区：认证中间件包裹；后续里程碑（生成/发布/选题/设置/日志）挂载于此
	protected := authSvc.Middleware(creatorSvc.Routes())
	mux.Handle("/api/v1/", protected)

	mux.Handle("/", web.SPAHandler(staticFS))

	// 中间件链：trace ID → panic 兜底
	handler := httpx.Recover(httpx.Trace(mux))

	addr := cfg.Host + ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅停机：SIGINT/SIGTERM → 5 秒内排空在途请求
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Siungo Creator Studio listening on http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("bye")
}
