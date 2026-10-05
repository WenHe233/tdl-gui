package app

import "strings"

// Windows may store either one endpoint or a protocol-specific endpoint list.
func proxyURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "=") {
		entries := map[string]string{}
		for _, part := range strings.Split(value, ";") {
			key, addr, ok := strings.Cut(part, "=")
			if ok {
				entries[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(addr)
			}
		}
		for _, protocol := range []string{"http", "https", "socks"} {
			if addr := entries[protocol]; addr != "" {
				if strings.Contains(addr, "://") {
					return addr
				}
				if protocol == "socks" {
					return "socks5://" + addr
				}
				return "http://" + addr
			}
		}
		return ""
	}
	if strings.Contains(value, "://") {
		return value
	}
	return "http://" + value
}
