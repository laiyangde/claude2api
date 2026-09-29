package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"claude2api/internal/config"
	"claude2api/internal/repository"
)

func TestNormalizeProxyURL(t *testing.T) {
	for _, address := range []string{
		"http://host:8080", "https://user:p%40ss@host:443", "socks5://host:1080", "socks5h://[::1]:1080",
	} {
		if _, err := NormalizeProxyURL(address); err != nil {
			t.Errorf("valid proxy rejected: %s: %v", address, err)
		}
	}
	for _, address := range []string{
		"", "host:8080", "ftp://host:21", "http://host", "http://host:0", "http://host:65536", "http://host:abc",
		"http://host:80/path", "http://host:80?query=1", "http://host:80#fragment", "http://user:bad password@host:80",
	} {
		if _, err := NormalizeProxyURL(address); err == nil {
			t.Errorf("invalid proxy accepted: %s", address)
		}
	}
	address, err := NormalizeProxyURL("  http://HOST:8080/  ")
	if err != nil || address != "http://host:8080" {
		t.Fatalf("normalization: %q %v", address, err)
	}
	if display := ProxyDisplayURL("http://user:secret@host:8080"); strings.Contains(display, "user") || strings.Contains(display, "secret") {
		t.Fatal("proxy display leaks credentials")
	}
}

/** A local CONNECT proxy routes Claude requests to a controlled TLS endpoint. */
func testExitProxy(t *testing.T, label string) string {
	t.Helper()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/account" {
			cookie, err := r.Cookie("sessionKey")
			if err != nil || cookie.Value == "" {
				http.Error(w, "missing session", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"email_address":%q,"memberships":[]}`, label+"@example.com")
			return
		}
		fmt.Fprint(w, label)
	}))
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		target, err := net.DialTimeout("tcp", upstream.Listener.Addr().String(), 3*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			target.Close()
			return
		}
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(target, conn); target.Close() }()
		_, _ = io.Copy(conn, target)
		conn.Close()
	}))
	t.Cleanup(func() { upstream.CloseClientConnections(); upstream.Close() })
	t.Cleanup(proxy.Close)
	return proxy.URL
}

func TestAccountProxyFallbackAndClientSwitch(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yaml", []byte("proxy: ''\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.Load()
	exitA, exitB := testExitProxy(t, "exit-a"), testExitProxy(t, "exit-b")
	config.Update(map[string]any{"proxy": exitA})
	acct := &repository.Account{Email: "test@example.com", Cookies: map[string]string{"sessionKey": "test-session"}}
	leaseA, err := clientFor(acct)
	if err != nil {
		t.Fatal(err)
	}
	info, err := leaseA.GetUserInfo()
	if err != nil || info.Email != "exit-a@example.com" {
		t.Fatalf("system exit: %+v %v", info, err)
	}
	id := uint(1)
	acct.ProxyID, acct.Proxy = &id, &repository.Proxy{ID: id, URL: exitB, Enabled: true}
	leaseB, err := clientFor(acct)
	if err != nil || leaseA == leaseB {
		t.Fatalf("switch did not replace cached client: %v", err)
	}
	info, err = leaseB.GetUserInfo()
	if err != nil || info.Email != "exit-b@example.com" {
		t.Fatalf("account exit: %+v %v", info, err)
	}
	again, _ := clientFor(acct)
	if again != leaseB {
		t.Fatal("unchanged proxy did not reuse client")
	}
	config.Update(map[string]any{"proxy": "http://127.0.0.1:1"})
	if address, err := AccountProxy(acct); err != nil || address != exitB {
		t.Fatal("global edit changed an explicit account exit")
	}
	acct.Proxy.URL = exitA
	updated, err := clientFor(acct)
	if err != nil || updated == leaseB || updated.proxy != exitA {
		t.Fatal("editing an assigned proxy did not replace client")
	}
	acct.Proxy = nil
	if _, err := clientFor(acct); err == nil {
		t.Fatal("missing assigned proxy silently used system exit")
	}
	acct.ProxyID = nil
	if address, err := AccountProxy(acct); err != nil || address != "http://127.0.0.1:1" {
		t.Fatal("clearing assignment did not restore system exit")
	}
	config.Update(map[string]any{"proxy": ""})
	if address, err := AccountProxy(acct); err != nil || address != "" {
		t.Fatal("empty system setting should connect directly")
	}
	leaseA.client.CloseIdleConnections()
	leaseB.client.CloseIdleConnections()
	updated.client.CloseIdleConnections()
	apiIndexLock.Lock()
	delete(apiClients, acct.Email)
	apiIndexLock.Unlock()
}

func TestMirrorConcurrentProxyIsolation(t *testing.T) {
	exits := []string{testExitProxy(t, "exit-a"), testExitProxy(t, "exit-b")}
	transport := NewProxyRoundTripper()
	t.Cleanup(func() {
		for _, client := range transport.clients {
			client.CloseIdleConnections()
		}
	})
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(WithOutboundProxy(ctx, exits[i%2]), http.MethodGet, "https://claude.ai/mirror", nil)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			want := []string{"exit-a", "exit-b"}[i%2]
			if string(body) != want {
				t.Errorf("wrong exit: got %q, want %q", body, want)
			}
		})
	}
	wg.Wait()
}

func TestInvalidProxyDoesNotFallBackToDirect(t *testing.T) {
	client := NewClaudeAI("test-key", "ftp://host:21", "test-account")
	if _, err := client.GetUserInfo(); err == nil {
		t.Fatal("invalid proxy did not stop the upstream request")
	}
	transport := NewProxyRoundTripper()
	req, _ := http.NewRequestWithContext(WithOutboundProxy(context.Background(), "ftp://host:21"), http.MethodGet, "https://claude.ai/", nil)
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("invalid mirror proxy did not stop the upstream request")
	}
}
