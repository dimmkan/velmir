package httpapi

import (
	"net"
	"net/http"
	"strings"

	"velmir/backend/internal/config"
)

func TrustedProxyHeaders(next http.Handler, proxies config.TrustedProxies) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		immediatePeer := remoteIPAddress(r.RemoteAddr)
		if immediatePeer == nil || !proxies.Contains(immediatePeer) {
			next.ServeHTTP(w, r)
			return
		}
		clientIP, ok := forwardedClientIP(r, proxies)
		if ok {
			clone := r.Clone(r.Context())
			clone.RemoteAddr = net.JoinHostPort(clientIP.String(), "0")
			r = clone
		}
		next.ServeHTTP(w, r)
	})
}

func forwardedClientIP(r *http.Request, proxies config.TrustedProxies) (net.IP, bool) {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		var candidate net.IP
		for index := len(parts) - 1; index >= 0; index-- {
			ip := net.ParseIP(strings.TrimSpace(parts[index]))
			if ip == nil {
				return nil, false
			}
			candidate = ip
			if !proxies.Contains(ip) {
				return candidate, true
			}
		}
		return candidate, candidate != nil
	}
	ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP")))
	return ip, ip != nil
}

func remoteIPAddress(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}
