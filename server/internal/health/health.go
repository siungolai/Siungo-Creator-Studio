// Package health 提供健康检查端点（探活/部署就绪探测用）。
package health

import (
	"net/http"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
)

type response struct {
	Status string `json:"status"`
}

// Handler 返回健康检查 handler：GET /api/v1/health → 200 {"status":"ok"}。
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response{Status: "ok"})
	})
}
