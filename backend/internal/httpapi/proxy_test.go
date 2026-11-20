package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"velmir/backend/internal/config"
)

func TestTrustedProxyHeaders(t *testing.T) {
	_, trustedNetwork, err := net.ParseCIDR("10.20.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	proxies := config.TrustedProxies{trustedNetwork}
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		realIP     string
		want       string
	}{
		{name: "untrusted peer ignores headers", remoteAddr: "192.0.2.10:1234", forwarded: "203.0.113.50", realIP: "203.0.113.51", want: "192.0.2.10"},
		{name: "trusted peer uses forwarded client", remoteAddr: "10.20.0.5:1234", forwarded: "203.0.113.50", want: "203.0.113.50"},
		{name: "trusted chain", remoteAddr: "10.20.0.5:1234", forwarded: "203.0.113.50, 10.20.0.8", want: "203.0.113.50"},
		{name: "forged leftmost entry ignored", remoteAddr: "10.20.0.5:1234", forwarded: "198.51.100.99, 203.0.113.50", want: "203.0.113.50"},
		{name: "invalid chain ignored", remoteAddr: "10.20.0.5:1234", forwarded: "not-an-ip", realIP: "203.0.113.51", want: "10.20.0.5"},
		{name: "real IP fallback", remoteAddr: "10.20.0.5:1234", realIP: "203.0.113.51", want: "203.0.113.51"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got string
			handler := TrustedProxyHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = remoteIPAddress(r.RemoteAddr).String()
			}), proxies)
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.forwarded)
			request.Header.Set("X-Real-IP", test.realIP)
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if got != test.want {
				t.Fatalf("client IP = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProxyHeadersIgnoredByDefault(t *testing.T) {
	var got string
	handler := TrustedProxyHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}), nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.50")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got != "192.0.2.10:1234" {
		t.Fatalf("RemoteAddr = %q", got)
	}
}
