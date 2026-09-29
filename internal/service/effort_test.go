package service

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
)

type effortHTTPClient struct {
	tlsclient.HttpClient
	do func(*fhttp.Request) (*fhttp.Response, error)
}

func (c *effortHTTPClient) Do(req *fhttp.Request) (*fhttp.Response, error) {
	return c.do(req)
}

func TestSendMessageEffort(t *testing.T) {
	client := &ClaudeAI{orgUUID: "org", client: &effortHTTPClient{}}
	for _, effort := range []string{"low", "medium", "high", "xhigh", ""} {
		t.Run("effort="+effort, func(t *testing.T) {
			called := false
			client.client.(*effortHTTPClient).do = func(req *fhttp.Request) (*fhttp.Response, error) {
				called = true
				if req.Method != fhttp.MethodPost || req.URL.Path != "/api/organizations/org/chat_conversations/conv/completion" {
					t.Fatalf("unexpected upstream request: %s %s", req.Method, req.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				value, present := body["effort"]
				want := effort
				if effort == "" {
					want = "low"
				}
				if !present || value != want {
					t.Errorf("upstream effort=%v, want %q", value, want)
				}
				if body["model"] != "claude-sonnet-5-5" || body["prompt"] != "hi" {
					t.Errorf("unexpected upstream payload: %v", body)
				}
				return &fhttp.Response{
					StatusCode: fhttp.StatusOK,
					Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")),
				}, nil
			}
			prompt := Prompt{Text: "hi", Effort: effort}
			var text strings.Builder
			status, err := client.SendMessage("conv", "claude-sonnet-5-5", prompt, nil, nil, func(chunk string) { text.WriteString(chunk) })
			if err != nil || status != fhttp.StatusOK || !called || text.String() != "ok" {
				t.Fatalf("status=%d called=%t output=%q err=%v", status, called, text.String(), err)
			}
		})
	}
}

func TestCreateConversationExtended(t *testing.T) {
	client := &ClaudeAI{orgUUID: "org", client: &effortHTTPClient{}}
	for _, model := range []string{"claude-sonnet-4-6", "claude-haiku-4-5-20251001", "claude-sonnet-5", "claude-sonnet-5-5", "claude-sonnet-5-5"} {
		t.Run(model, func(t *testing.T) {
			calls := 0
			client.client.(*effortHTTPClient).do = func(req *fhttp.Request) (*fhttp.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				switch calls {
				case 1:
					if req.Method != fhttp.MethodPut || req.URL.Path != "/api/account" {
						t.Fatalf("expected account settings before creation, got %s %s", req.Method, req.URL)
					}
					settings, ok := body["settings"].(map[string]any)
					if !ok || settings["paprika_mode"] != "extended" {
						t.Fatalf("expected extended mode: %v", body)
					}
				case 2:
					if req.Method != fhttp.MethodPost || req.URL.Path != "/api/organizations/org/chat_conversations" || body["model"] != model {
						t.Fatalf("unexpected conversation request: %s %s %v", req.Method, req.URL, body)
					}
				default:
					t.Fatalf("unexpected extra request: %s", req.URL)
				}
				return &fhttp.Response{StatusCode: fhttp.StatusOK, Body: io.NopCloser(strings.NewReader(`{"uuid":"conv"}`))}, nil
			}
			convID, err := client.CreateConversation(model)
			if err != nil || convID != "conv" || calls != 2 {
				t.Fatalf("convID=%q calls=%d err=%v", convID, calls, err)
			}
		})
	}
}

func TestCreateConversationRequiresExtended(t *testing.T) {
	calls := 0
	client := &ClaudeAI{orgUUID: "org", client: &effortHTTPClient{
		do: func(req *fhttp.Request) (*fhttp.Response, error) {
			calls++
			if req.Method != fhttp.MethodPut || req.URL.Path != "/api/account" {
				t.Fatalf("conversation created without extended mode: %s %s", req.Method, req.URL)
			}
			return &fhttp.Response{StatusCode: fhttp.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		},
	}}
	convID, err := client.CreateConversation("claude-sonnet-5-5")
	if err == nil || convID != "" || calls != 1 {
		t.Fatalf("convID=%q calls=%d err=%v", convID, calls, err)
	}
}
