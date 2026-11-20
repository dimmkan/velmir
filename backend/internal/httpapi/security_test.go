package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	for _, test := range []struct {
		name          string
		productionTLS bool
		wantHSTS      bool
	}{
		{name: "development", productionTLS: false, wantHSTS: false},
		{name: "production HTTPS", productionTLS: true, wantHSTS: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), test.productionTLS).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			header := response.Header()
			if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("X-Frame-Options") != "DENY" || header.Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("security headers = %v", header)
			}
			csp := header.Get("Content-Security-Policy")
			if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
				t.Fatalf("CSP = %q", csp)
			}
			if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
				t.Fatalf("style CSP = %q", csp)
			}
			if got := header.Get("Strict-Transport-Security"); (got != "") != test.wantHSTS || strings.Contains(got, "includeSubDomains") || strings.Contains(got, "preload") {
				t.Fatalf("HSTS = %q", got)
			}
			if header.Get("Permissions-Policy") == "" {
				t.Fatal("Permissions-Policy is missing")
			}
		})
	}
}
