// Package httpx 提供统一 JSON 响应、错误格式与请求级中间件（Trace/Recover）。
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
)

// WriteJSON 统一成功响应（Content-Type: application/json）。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("httpx: encode response: %v", err)
	}
}

// WriteError 统一错误响应：{"error": "<message>"}（业务语义由 HTTP 状态码表达）。
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

type ctxKey struct{}

// Trace 为每个请求生成 trace ID：注入 context、写入响应头 X-Trace-Id，
// 供日志/错误兜底全链路关联（单实例工具，8 字节随机 hex 足够）。
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := traceID()
		w.Header().Set("X-Trace-Id", tid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, tid)))
	})
}

// TraceID 从 context 取出当前请求的 trace ID（无则空串）。
func TraceID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// Recover 兜底 panic：记录日志（含 trace ID）并返回 500 JSON，避免连接无痕中断。
// 注意：若 panic 前已写入响应头，WriteError 将无法生效，但日志始终保留。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("httpx: panic recovered trace=%s path=%s err=%v", TraceID(r.Context()), r.URL.Path, rec)
				WriteError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func traceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
