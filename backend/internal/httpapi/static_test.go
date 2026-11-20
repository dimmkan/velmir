package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandlerSeparatesAPIAndSPA(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStaticTestFile(t, root, "index.html", "<html>spa</html>")
	writeStaticTestFile(t, root, "assets/app-a1b2c3.js", "console.log('ok')")
	writeStaticTestFile(t, root, "favicon.svg", "<svg/>")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND"}}`))
	})
	handler, err := NewStaticHandler(api, root)
	if err != nil {
		t.Fatal(err)
	}

	for _, apiPath := range []string{"/api/unknown", "/api", "/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, apiPath, nil))
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "NOT_FOUND") {
			t.Errorf("%s response = %d %s", apiPath, response.Code, response.Body.String())
		}
	}
	for _, frontendPath := range []string{"/", "/login", "/resources/public/books"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, frontendPath, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "spa") {
			t.Errorf("%s response = %d %s", frontendPath, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s Cache-Control = %q", frontendPath, got)
		}
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app-a1b2c3.js", nil))
	if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("asset response = %d, cache = %q", asset.Code, asset.Header().Get("Cache-Control"))
	}
	missingAsset := httptest.NewRecorder()
	handler.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missingAsset.Code != http.StatusNotFound || strings.Contains(missingAsset.Body.String(), "spa") {
		t.Errorf("missing asset response = %d %s", missingAsset.Code, missingAsset.Body.String())
	}
}

func TestStaticHandlerRequiresBuiltIndex(t *testing.T) {
	if _, err := NewStaticHandler(http.NotFoundHandler(), t.TempDir()); err == nil {
		t.Fatal("NewStaticHandler accepted a directory without index.html")
	}
}

func TestLimitJSONBody(t *testing.T) {
	handler := LimitJSONBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 16)
		if _, err := r.Body.Read(buffer); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), 8)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"too-long"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", response.Code)
	}
}

func writeStaticTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
