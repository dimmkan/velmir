package httpapi

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func NewStaticHandler(api http.Handler, distPath string) (http.Handler, error) {
	root, err := filepath.Abs(distPath)
	if err != nil {
		return nil, errors.New("resolve FRONTEND_DIST_PATH")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, errors.New("FRONTEND_DIST_PATH must point to the built frontend directory")
	}
	indexInfo, err := os.Stat(filepath.Join(root, "index.html"))
	if err != nil || !indexInfo.Mode().IsRegular() {
		return nil, errors.New("FRONTEND_DIST_PATH does not contain index.html")
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(cleanPath, "/")
		serveIndex := name == "" || name == "index.html"
		if !serveIndex {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
			serveIndex = errors.Is(err, os.ErrNotExist) && path.Ext(name) == ""
			if err == nil && !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			if err != nil && !serveIndex {
				http.NotFound(w, r)
				return
			}
		}
		if serveIndex {
			name = "index.html"
			r = cloneRequestPath(r, "/")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}), nil
}

func LimitJSONBody(next http.Handler, maximum int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && isJSONContentType(r.Header.Get("Content-Type")) {
			r.Body = http.MaxBytesReader(w, r.Body, maximum)
		}
		next.ServeHTTP(w, r)
	})
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && (mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"))
}

func cloneRequestPath(r *http.Request, newPath string) *http.Request {
	clone := r.Clone(r.Context())
	urlCopy := *r.URL
	urlCopy.Path = newPath
	urlCopy.RawPath = ""
	clone.URL = &urlCopy
	return clone
}
