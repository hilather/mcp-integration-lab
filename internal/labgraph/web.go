package labgraph

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/index.html
var webFS embed.FS

func SPA() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "ui unavailable", http.StatusInternalServerError)
		})
	}
	return http.FileServer(http.FS(sub))
}
