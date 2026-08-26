// Package web 提供单二进制静态资源服务（go:embed 前端构建产物 + SPA 回退）。
package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler 服务嵌入的静态资源：存在的文件直接返回，其余路径回退 index.html（React Router 路由）。
// /api/* 不应到达这里（由上层路由先匹配）。
func SPAHandler(fsys fs.FS) http.Handler {
	sub, err := fs.Sub(fsys, "static")
	if err != nil {
		// static 目录缺失属构建配置错误，启动即暴露
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := sub.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA 回退：始终返回 index.html（尚未构建时为占位提示）
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
