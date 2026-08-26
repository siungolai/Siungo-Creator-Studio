package creator

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/siungolai/Siungo-Creator-Studio/server/internal/httpx"
)

// Routes 组装 creator 全部路由（挂载于受保护 /api/v1 区，见 main.go）。
func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/creator/works", s.listWorksHandler())
	mux.Handle("POST /api/v1/creator/works", s.createWorkHandler())
	mux.Handle("GET /api/v1/creator/works/{id}", s.getWorkHandler())
	mux.Handle("PUT /api/v1/creator/works/{id}", s.updateWorkHandler())
	mux.Handle("DELETE /api/v1/creator/works/{id}", s.deleteWorkHandler())
	mux.Handle("GET /api/v1/creator/platforms", s.listPlatformsHandler())
	return mux
}

// parseID 解析路径 {id}；非法返回 false（调用方回 400）。
func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Service) getWorkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		work, err := s.GetWork(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "work not found")
				return
			}
			log.Printf("creator: get work: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, work)
	})
}

func (s *Service) updateWorkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		// 工作副本可能较长（AI 脚本），body 上限放宽到 256KB
		var req UpdateWorkRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		work, err := s.UpdateWork(r.Context(), id, req)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				httpx.WriteError(w, http.StatusBadRequest, err.Error())
				return
			}
			if errors.Is(err, ErrNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "work not found")
				return
			}
			log.Printf("creator: update work: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, work)
	})
}

func (s *Service) listWorksHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 查询参数：status / q / limit / offset（T5 分页过滤规范）
		q := r.URL.Query()
		filter := WorkFilter{Status: q.Get("status"), Query: q.Get("q")}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				httpx.WriteError(w, http.StatusBadRequest, "invalid limit")
				return
			}
			filter.Limit = n
		}
		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				httpx.WriteError(w, http.StatusBadRequest, "invalid offset")
				return
			}
			filter.Offset = n
		}
		page, err := s.ListWorks(r.Context(), filter)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				httpx.WriteError(w, http.StatusBadRequest, err.Error())
				return
			}
			log.Printf("creator: list works: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page)
	})
}

func (s *Service) createWorkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CreateWorkRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		work, err := s.CreateWork(r.Context(), req)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				httpx.WriteError(w, http.StatusBadRequest, err.Error())
				return
			}
			log.Printf("creator: create work: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, work)
	})
}

func (s *Service) deleteWorkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := s.DeleteWork(r.Context(), id); err != nil {
			if errors.Is(err, ErrNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "work not found")
				return
			}
			log.Printf("creator: delete work: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Service) listPlatformsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platforms, err := s.ListPlatforms(r.Context())
		if err != nil {
			log.Printf("creator: list platforms: %v trace=%s", err, httpx.TraceID(r.Context()))
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, platforms)
	})
}
