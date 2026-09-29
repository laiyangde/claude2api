package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseImportWithOptionalProxy(t *testing.T) {
	items, err := parseImportSessionKeys("\r\n key-a \r\nkey-b\thttp://user:pass@HOST:8080/\r\nkey-a\nkey-b http://user:pass@host:8080\nkey-c socks5h://localhost:1080")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].proxy != "" || items[0].line != 2 || items[1].sessionKey != "key-b" || items[1].proxy != "http://user:pass@host:8080" || items[2].proxy != "socks5h://localhost:1080" {
		t.Fatalf("unexpected parsed rows: %+v", items)
	}
}

func TestImportRejectsAmbiguousRows(t *testing.T) {
	for _, input := range []string{
		"key\nkey http://host:8080",
		"key http://one:8080\nkey http://two:8080",
		"key\nkey2 ftp://host:80",
		"key\nkey2 http://host:8080 extra",
		"key\nkey2 this-is-not-a-proxy",
	} {
		_, err := parseImportSessionKeys(input)
		if err == nil || !strings.Contains(err.Error(), "第 2 行") {
			t.Errorf("expected a line-specific error, got %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "key2") {
			t.Fatal("validation error exposes a session key")
		}
	}
}

func TestInvalidProxyRequestsFailBeforeStreamingOrStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/import", AdminImportAccounts)
	r.POST("/proxies", AdminSaveProxy)
	r.POST("/account-proxy", AdminSetAccountProxy)
	r.POST("/check", AdminCheckProxy)
	r.POST("/enabled", AdminSetProxyEnabled)
	for _, tc := range []struct{ path, body string }{
		{"/import", `{"session_keys":"key invalid-proxy"}`},
		{"/import", `{"session_keys":"\n \r\n"}`},
		{"/proxies", `{"url":"http://user:secret@host:99999"}`},
		{"/proxies", `{"url":"file:///tmp/proxy"}`},
		{"/account-proxy", `{"email":"user@example.com","proxy_id":-1}`},
		{"/check", `{"id":0}`},
		{"/enabled", `{"id":1}`},
		{"/enabled", `{"id":1,"enabled":"false"}`},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") || strings.Contains(recorder.Body.String(), "secret") {
			t.Fatalf("%s: %d %s", tc.path, recorder.Code, recorder.Body.String())
		}
	}
}
