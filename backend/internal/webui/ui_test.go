package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleRoutesKeepAPIMissesAndAssetsDistinct(t *testing.T) {
	directory := t.TempDir()
	for name, content := range map[string]string{"index.html": "<h1>Avior</h1>", "app.js": "console.log('asset')"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := WithUI(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "API miss", 404) }), directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/query", 200, "Avior"},
		{"GET", "/app.js", 200, "asset"},
		{"GET", "/missing.js", 404, "404"},
		{"GET", "/.well-known/oauth-authorization-server", 404, "404"},
		{"GET", "/.vite/modules.json", 404, "404"},
		{"POST", "/register", 404, "404"},
		{"GET", "/api/v1/missing", 404, "API miss"},
		{"GET", "/mcp", 404, "API miss"},
		{"GET", "/api", 404, "API miss"},
		{"GET", "/mcp/missing", 404, "API miss"},
		{"HEAD", "/query", 200, ""},
		{"POST", "/query", 405, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			if tc.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
	if _, err := WithUI(handler, directory+"/missing"); err == nil {
		t.Fatal("missing UI accepted")
	}
	unchanged, err := WithUI(handler, "")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	unchanged.ServeHTTP(response, httptest.NewRequest("GET", "/query", nil))
	if response.Code != http.StatusOK {
		t.Fatal("empty directory changed the API handler")
	}
	if err := os.Remove(filepath.Join(directory, "index.html")); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/query", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("missing console returned success")
	}
}
