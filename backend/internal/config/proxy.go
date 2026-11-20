package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

type TrustedProxies []*net.IPNet

func trustedProxiesFromEnv() (TrustedProxies, error) {
	value := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if value == "" {
		return nil, nil
	}
	var proxies TrustedProxies
	for _, item := range strings.Split(value, ",") {
		cidr := strings.TrimSpace(item)
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain comma-separated CIDRs")
		}
		proxies = append(proxies, network)
	}
	return proxies, nil
}

func (proxies TrustedProxies) Contains(ip net.IP) bool {
	for _, network := range proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
