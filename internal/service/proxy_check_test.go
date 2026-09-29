package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"claude2api/internal/repository"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
)

type checkHTTPClient struct {
	tlsclient.HttpClient
	do func(*fhttp.Request) (*fhttp.Response, error)
}

func (c checkHTTPClient) Do(r *fhttp.Request) (*fhttp.Response, error) { return c.do(r) }

func TestProxyProbeResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		err  error
		ok   bool
		ip   string
	}{
		{"IPv4", 200, `{"ip":"203.0.113.10"}`, nil, true, "203.0.113.10"},
		{"IPv6", 200, `{"ip":"2001:db8::1"}`, nil, true, "2001:db8::1"},
		{"http failure", 503, `unavailable`, nil, false, ""},
		{"redirect", 302, "", nil, false, ""},
		{"invalid IP", 200, `{"ip":"not-an-ip"}`, nil, false, ""},
		{"invalid JSON", 200, `<html>blocked</html>`, nil, false, ""},
		{"timeout", 0, "", context.DeadlineExceeded, false, ""},
		{"network", 0, "", errors.New("http://username:secret@host:80 failed"), false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := checkHTTPClient{do: func(req *fhttp.Request) (*fhttp.Response, error) {
				if req.URL.String() != proxyCheckURL || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
					t.Fatal("probe target or credentials are incorrect")
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &fhttp.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			result := probeProxyClient(context.Background(), client)
			if result.OK != tc.ok || result.ExitIP != tc.ip || result.StatusCode != tc.code || result.CheckedAt.IsZero() {
				t.Fatalf("unexpected check: %+v", result)
			}
			if !tc.ok && result.Error == "" || strings.Contains(result.Error, "secret") {
				t.Fatalf("unsafe or absent diagnostic: %+v", result)
			}
			if tc.name == "timeout" && !strings.Contains(result.Error, "超时") {
				t.Fatal("timeout not identified")
			}
		})
	}
}

func TestAccountSelectionHonorsProxyHealth(t *testing.T) {
	id := uint(1)
	p := &repository.Proxy{ID: id, URL: "http://host:8080", Enabled: true}
	a := &repository.Account{Cookies: map[string]string{"sessionKey": "key"}, ProxyID: &id, Proxy: p}
	if !AccountUsable(a) {
		t.Fatal("unprobed proxy must be eligible for automatic detection")
	}
	p.Enabled = false
	if AccountUsable(a) {
		t.Fatal("disabled proxy eligible")
	}
	if _, err := AccountProxy(a); !errors.Is(err, repository.ErrProxyDisabled) {
		t.Fatal("disabled proxy silently accepted")
	}
	p.Enabled = true
	now, ok := time.Now(), false
	p.LastCheckedAt, p.LastCheckOK = &now, &ok
	if AccountUsable(a) {
		t.Fatal("recently failed proxy eligible")
	}
	expired := time.Now().Add(-ProxyCheckTTL - time.Second)
	p.LastCheckedAt = &expired
	if !AccountUsable(a) {
		t.Fatal("stale failure cannot be rechecked")
	}
	p.LastCheckedAt, ok = &now, true
	if !AccountUsable(a) {
		t.Fatal("healthy proxy excluded")
	}
}
