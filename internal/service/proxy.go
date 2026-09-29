package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"claude2api/internal/config"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

type outboundProxyKey struct{}

func WithOutboundProxy(ctx context.Context, proxy string) context.Context {
	return context.WithValue(ctx, outboundProxyKey{}, proxy)
}

type ProxyRoundTripper struct {
	mu      sync.Mutex
	clients map[string]tlsclient.HttpClient
}

func NewProxyRoundTripper() *ProxyRoundTripper {
	return &ProxyRoundTripper{clients: make(map[string]tlsclient.HttpClient)}
}

/** Each cached transport has an immutable exit, isolating concurrent requests. */
func (p *ProxyRoundTripper) clientFor(proxy string) (tlsclient.HttpClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if hc := p.clients[proxy]; hc != nil {
		return hc, nil
	}
	hc, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		/** Keep long-running mirror SSE connections open. */
		tlsclient.WithTimeoutSeconds(3600),
		tlsclient.WithInsecureSkipVerify(),
		tlsclient.WithProxyUrl(proxy),
		tlsclient.WithNotFollowRedirects())
	if err != nil {
		return nil, fmt.Errorf("初始化出口代理失败，请检查代理配置")
	}
	hc.SetCookieJar(nil)
	/** Bound stale transports without interrupting active streams. */
	if len(p.clients) >= 128 {
		for key, old := range p.clients {
			old.CloseIdleConnections()
			delete(p.clients, key)
			break
		}
	}
	p.clients[proxy] = hc
	return hc, nil
}

func (p *ProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	proxy, ok := req.Context().Value(outboundProxyKey{}).(string)
	if !ok {
		proxy = config.Get().Proxy
	}
	hc, err := p.clientFor(proxy)
	if err != nil {
		return nil, err
	}
	fr, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, err
	}
	for k, values := range req.Header {
		for _, value := range values {
			fr.Header.Add(k, value)
		}
	}
	fr.Host, fr.ContentLength = req.Host, req.ContentLength
	resp, err := hc.Do(fr)
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	for k, values := range resp.Header {
		for _, value := range values {
			h.Add(k, value)
		}
	}
	return &http.Response{Status: resp.Status, StatusCode: resp.StatusCode, Proto: resp.Proto,
		ProtoMajor: resp.ProtoMajor, ProtoMinor: resp.ProtoMinor, Header: h, Body: resp.Body,
		ContentLength: resp.ContentLength, Request: req}, nil
}
