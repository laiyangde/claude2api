package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"claude2api/internal/repository"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/sync/singleflight"
)

const ProxyCheckTTL = 5 * time.Minute
const proxyCheckTimeout = 12 * time.Second

/** Test builds can target a local probe server without changing production routing. */
var proxyCheckURL = "https://api64.ipify.org?format=json"

var proxyChecks singleflight.Group
var proxyCheckSlots = make(chan struct{}, 4)

func ProxyCheckFresh(p *repository.Proxy) bool {
	return p != nil && p.LastCheckedAt != nil && time.Since(*p.LastCheckedAt) >= 0 && time.Since(*p.LastCheckedAt) < ProxyCheckTTL
}

/** Probes use an isolated client without account cookies or API credentials. */
func probeProxy(ctx context.Context, address string) repository.ProxyCheckResult {
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_146), tlsclient.WithProxyUrl(address),
		tlsclient.WithTimeoutSeconds(int(proxyCheckTimeout/time.Second)), tlsclient.WithNotFollowRedirects())
	if err != nil {
		return repository.ProxyCheckResult{CheckedAt: time.Now().UTC(), Error: "代理配置无效"}
	}
	defer client.CloseIdleConnections()
	return probeProxyClient(ctx, client)
}

func probeProxyClient(ctx context.Context, client tlsclient.HttpClient) (result repository.ProxyCheckResult) {
	start := time.Now()
	defer func() { result.CheckedAt, result.LatencyMS = time.Now().UTC(), time.Since(start).Milliseconds() }()
	req, err := fhttp.NewRequestWithContext(ctx, fhttp.MethodGet, proxyCheckURL, nil)
	if err != nil {
		result.Error = "无法创建检测请求"
		return
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded || errors.As(err, &networkError) && networkError.Timeout() {
			result.Error = "检测超时（12 秒）"
		} else {
			result.Error = "连接失败，请检查代理地址、认证或网络"
		}
		return
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode != fhttp.StatusOK {
		result.Error = fmt.Sprintf("检测服务返回 HTTP %d", resp.StatusCode)
		return
	}
	var body struct {
		IP string `json:"ip"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	if err != nil || net.ParseIP(body.IP) == nil {
		result.Error = "检测服务未返回有效的出口 IP"
		return
	}
	result.OK, result.ExitIP = true, body.IP
	return result
}

func CheckProxy(ctx context.Context, id uint, force bool) (*repository.Proxy, error) {
	p, err := repository.ProxyByID(id)
	if err != nil {
		return nil, err
	}
	if !force && ProxyCheckFresh(p) {
		return p, nil
	}
	key := fmt.Sprintf("%d:%d", p.ID, p.CheckRevision)
	result := proxyChecks.DoChan(key, func() (any, error) {
		/** One caller cancelling must not cancel a check shared by other accounts. */
		checkCtx, cancel := context.WithTimeout(context.Background(), proxyCheckTimeout)
		defer cancel()
		select {
		case proxyCheckSlots <- struct{}{}:
			defer func() { <-proxyCheckSlots }()
		case <-checkCtx.Done():
			return nil, fmt.Errorf("检测繁忙，请稍后重试")
		}
		current, err := repository.ProxyByID(id)
		if err != nil {
			return nil, err
		}
		if current.CheckRevision != p.CheckRevision {
			return nil, repository.ErrProxyChanged
		}
		if !force && ProxyCheckFresh(current) {
			return current, nil
		}
		check := probeProxy(checkCtx, current.URL)
		if err := repository.SaveProxyCheck(current, check); err != nil {
			return nil, err
		}
		return repository.ProxyByID(id)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case checked := <-result:
		if checked.Err != nil {
			return nil, checked.Err
		}
		return checked.Val.(*repository.Proxy), nil
	}
}

func EnsureAccountProxy(ctx context.Context, account *repository.Account) (string, error) {
	if _, err := AccountProxy(account); err != nil {
		return "", err
	}
	if account == nil || account.ProxyID == nil {
		return AccountProxy(account)
	}
	p, err := CheckProxy(ctx, *account.ProxyID, false)
	if err != nil {
		return "", err
	}
	account.Proxy = p
	if !p.Enabled {
		return "", repository.ErrProxyDisabled
	}
	if p.LastCheckOK == nil || !*p.LastCheckOK {
		return "", fmt.Errorf("账号代理检测不可用：%s", p.LastCheckError)
	}
	return p.URL, nil
}
