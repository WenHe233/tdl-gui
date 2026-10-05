package app

import "golang.org/x/sys/windows/registry"

func systemProxy() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return proxyURL(server)
}
