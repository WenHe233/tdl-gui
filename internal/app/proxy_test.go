package app

import "testing"

func TestWindowsProxyFormats(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "127.0.0.1:7890": "http://127.0.0.1:7890",
		"http=127.0.0.1:7890;https=127.0.0.1:7891": "http://127.0.0.1:7890",
		"socks=127.0.0.1:1080":                     "socks5://127.0.0.1:1080",
		"https=127.0.0.1:7890":                     "http://127.0.0.1:7890",
		"socks5://127.0.0.1:1080":                  "socks5://127.0.0.1:1080",
		"ftp=localhost:21":                         "",
	} {
		if got := proxyURL(input); got != want {
			t.Errorf("proxyURL(%q)=%q; want %q", input, got, want)
		}
	}
}
