// Package webui serves the built console beside the API in the standalone image.
package webui

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// WithUI keeps API and MCP requests on their existing handler, including misses.
// Only extensionless browser routes fall back to the SPA entry point.
func WithUI(api http.Handler, directory string) (http.Handler, error) {
	if directory == "" {
		return api, nil
	}
	files := os.DirFS(directory)
	index, err := fs.Stat(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read the built console: %w", err)
	}
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			api.ServeHTTP(w, r)
			return
		}
		// Match the split frontend's OAuth discovery refusals. A SPA response
		// here makes MCP clients try to parse HTML as authorization metadata.
		if strings.HasPrefix(r.URL.Path, "/.") || r.URL.Path == "/register" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(files, name)
		if err != nil || info.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			// ServeContent handles HEAD and conditional requests without redirecting
			// the browser away from the route it requested.
			entry, readErr := os.Open(directory + "/index.html")
			if readErr != nil {
				http.Error(w, "Console unavailable.", http.StatusServiceUnavailable)
				return
			}
			defer func() { _ = entry.Close() }()
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", index.ModTime(), entry)
			return
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		server.ServeHTTP(w, r)
	}), nil
}

// API misses belong to the API handler rather than the SPA fallback.
func isAPIPath(route string) bool {
	return route == "/api" || strings.HasPrefix(route, "/api/") ||
		route == "/mcp" || strings.HasPrefix(route, "/mcp/")
}
