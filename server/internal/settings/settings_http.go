package settings

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
)

// settingsResponse GET 全量返回；提示词未设置时返回空串（前端展示默认占位文案）。
type settingsResponse struct {
	AITimeoutSeconds          int    `json:"aiTimeoutSeconds"`
	GenerateSystemPrompt      string `json:"generateSystemPrompt"`
	GenerateUserPromptTemplate string `json:"generateUserPromptTemplate"`
}

// settingsRequest PUT 部分更新：指针字段区分「未提供（不更新）」与「空串（恢复默认）」，
// 保证只传 aiTimeoutSeconds 的既有调用不会误清提示词设置。
type settingsRequest struct {
	AITimeoutSeconds           *int    `json:"aiTimeoutSeconds"`
	GenerateSystemPrompt       *string `json:"generateSystemPrompt"`
	GenerateUserPromptTemplate *string `json:"generateUserPromptTemplate"`
}

// Handler 组装设置路由（GET/PUT /api/v1/creator/settings，挂载于 creator 模块路由区）。
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/creator/settings", s.getHandler())
	mux.Handle("PUT /api/v1/creator/settings", s.putHandler())
	return mux
}

func (s *Service) getHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		timeout, err := s.AITimeoutSeconds(ctx)
		if err != nil {
			settingsLog("get timeout", err, r)
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		sys, err := s.GenerateSystemPrompt(ctx)
		if err != nil {
			settingsLog("get sys prompt", err, r)
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		userTpl, err := s.GenerateUserPromptTemplate(ctx)
		if err != nil {
			settingsLog("get user prompt tpl", err, r)
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, settingsResponse{
			AITimeoutSeconds:           timeout,
			GenerateSystemPrompt:       sys,
			GenerateUserPromptTemplate: userTpl,
		})
	})
}

func (s *Service) putHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req settingsRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		ctx := r.Context()
		if req.AITimeoutSeconds != nil {
			if err := s.SetAITimeoutSeconds(ctx, *req.AITimeoutSeconds); err != nil {
				writeSetErr(w, r, err)
				return
			}
		}
		if req.GenerateSystemPrompt != nil {
			if err := s.SetGenerateSystemPrompt(ctx, *req.GenerateSystemPrompt); err != nil {
				writeSetErr(w, r, err)
				return
			}
		}
		if req.GenerateUserPromptTemplate != nil {
			if err := s.SetGenerateUserPromptTemplate(ctx, *req.GenerateUserPromptTemplate); err != nil {
				writeSetErr(w, r, err)
				return
			}
		}
		// 回读最新值返回（保持 GET 同构，前端可直接展示）
		timeout, _ := s.AITimeoutSeconds(ctx)
		sys, _ := s.GenerateSystemPrompt(ctx)
		userTpl, _ := s.GenerateUserPromptTemplate(ctx)
		httpx.WriteJSON(w, http.StatusOK, settingsResponse{
			AITimeoutSeconds:           timeout,
			GenerateSystemPrompt:       sys,
			GenerateUserPromptTemplate: userTpl,
		})
	})
}

func writeSetErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrInvalid) {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	settingsLog("set", err, r)
	httpx.WriteError(w, http.StatusInternalServerError, "internal error")
}

func settingsLog(op string, err error, r *http.Request) {
	log.Printf("settings: %s: %v trace=%s", op, err, httpx.TraceID(r.Context()))
}
