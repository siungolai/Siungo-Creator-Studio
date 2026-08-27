package ai

import (
	"encoding/json"
	"net/http"
	"strings"
)

// StatusHandler GET /api/v1/ai/status —— 当前 AI 配置状态。
// 只暴露"是否已配置"与模型名，绝不返回 key 本身。
func StatusHandler(svc *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{
			HasAPIKey: svc.GetEffectiveProvider() != nil,
			Model:     svc.EffectiveModel(),
		})
	})
}

// ConfigureHandler POST /api/v1/ai/configure {"api_key":"sk-..."} 写入内存 key；
// {"api_key":""} 显式清除。key 仅存内存，重启即失效；前后端均不落盘、不落日志。
func ConfigureHandler(svc *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req configureRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		key := strings.TrimSpace(req.APIKey)
		mgr := svc.GetAPIKeyManager()
		if mgr == nil {
			writeError(w, http.StatusInternalServerError, "ai key manager not attached")
			return
		}

		if key == "" {
			mgr.ClearAPIKey()
			writeJSON(w, http.StatusOK, configureResponse{Message: "API Key 已清除"})
			return
		}

		// 预校验：构造 provider 失败说明 key 格式异常（deepseek-go 允许任意非空串，
		// 故此处基本恒过；真实有效性由首次生成调用暴露）
		if _, err := NewDeepSeekProvider(key, svc.baseURL, svc.model); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		mgr.SetAPIKey(key)
		writeJSON(w, http.StatusOK, configureResponse{Message: "API Key 已保存（仅本次运行有效）"})
	})
}

// ---- 请求/响应体与 JSON 工具 ----

type statusResponse struct {
	HasAPIKey bool   `json:"has_api_key"`
	Model     string `json:"model"`
}

type configureRequest struct {
	APIKey string `json:"api_key"`
}

type configureResponse struct {
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
