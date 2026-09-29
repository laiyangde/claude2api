package service

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"claude2api/internal/config"
	"claude2api/internal/repository"
)

func NormalizeProxyURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || strings.ContainsAny(address, "?#") || strings.IndexFunc(address, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("代理地址格式错误，请填写 协议://[用户名:密码@]主机:端口")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("代理协议仅支持 http、https、socks5、socks5h")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("代理地址不能包含路径")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("代理端口必须在 1–65535 之间")
		}
	} else {
		return "", fmt.Errorf("代理地址必须包含端口")
	}
	u.Path = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func ProxyDisplayURL(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}

/** Resolve once per request so a proxy edit takes effect on the next request. */
func AccountProxy(account *repository.Account) (string, error) {
	if account == nil || account.ProxyID == nil {
		return config.Get().Proxy, nil
	}
	if account.Proxy == nil || account.Proxy.URL == "" {
		return "", fmt.Errorf("账号代理不存在，请重新选择代理")
	}
	if !account.Proxy.Enabled {
		return "", repository.ErrProxyDisabled
	}
	return account.Proxy.URL, nil
}
