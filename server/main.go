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

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/ai"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/auth"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/creator"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/db"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/health"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/settings"
	"github.com/siungolai/Siungo-Creator-Studio/server/internal/web"
)

//go:embed static
var staticFS embed.FS

// checkAIStatus 启动时报告 AI 服务状态（不输出任何 key 内容）。
func checkAIStatus(aiSvc *ai.Service) {
	if aiSvc.GetEffectiveProvider() != nil {
		log.Printf("ai: ready（model=%s）", aiSvc.EffectiveModel())
		return
	}
	log.Printf("ai: API Key 未配置，AI 功能不可用——请登录后在「AI 设置」中输入 DeepSeek API Key")
}

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

	settingsSvc := settings.NewService(settings.NewStore(conn))

	// AI 装配：key 不再走环境变量，运行时经网页「AI 设置」写入内存（重启/清空即失效）
	apiKeyMgr := ai.NewAPIKeyManager()
	aiSvc := ai.NewService(nil, ai.NewAuditStore(conn), settingsSvc)
	aiSvc.AttachAPIKeyManager(apiKeyMgr, cfg.AIBaseURL, cfg.AIModel)
	checkAIStatus(aiSvc)

	creatorSvc := creator.NewService(creator.NewSQLiteStore(conn), aiSvc)
	creatorSvc.AttachPromptSettings(settingsSvc) // Issue 18：生成提示词可覆盖（settings 表）

	mux := http.NewServeMux()
	mux.Handle("/api/v1/health", health.Handler())
	mux.Handle("/api/v1/login", authSvc.LoginHandler())
	mux.Handle("/api/v1/logout", authSvc.LogoutHandler())
	mux.Handle("/api/v1/me", authSvc.MeHandler())

	// AI Key 配置端点：受认证保护（key 属敏感项，未登录不可探测/写入）
	mux.Handle("GET /api/v1/ai/status", authSvc.Middleware(ai.StatusHandler(aiSvc)))
	mux.Handle("POST /api/v1/ai/configure", authSvc.Middleware(ai.ConfigureHandler(aiSvc)))

	// 受保护 API 区：认证中间件包裹；后续里程碑（生成/发布/选题/日志）挂载于此
	protected := authSvc.Middleware(creatorSvc.Routes(settingsSvc.Handler()))
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
