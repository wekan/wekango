package web

import (
	"embed"
	"net/http"
)

//go:embed index.html
var files embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/sign-in" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		data, _ := files.ReadFile("index.html")
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	})
}
