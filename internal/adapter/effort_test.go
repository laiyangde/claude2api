package adapter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude2api/internal/service"

	"github.com/gin-gonic/gin"
)

func TestEffortAPI(t *testing.T) {
	old := runner
	t.Cleanup(func() { runner = old })
	routes := []struct {
		name, path, input, field, container string
		handler                             gin.HandlerFunc
		allowMax                            bool
	}{
		{"chat", "/v1/chat/completions", `"messages":[{"role":"user","content":"hi"}]`, "reasoning_effort", "", OpenAIChat, false},
		{"responses", "/v1/responses", `"input":"hi"`, "effort", "reasoning", OpenAIResponses, false},
		{"messages", "/v1/messages", `"messages":[{"role":"user","content":"hi"}]`, "effort", "output_config", AnthropicMessages, true},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			cases := []struct {
				name, native, direct, want string
				invalid                    bool
			}{
				{"omitted", "", "", "low", false},
				{"null", "null", "null", "low", false},
				{"native null", "null", `"low"`, "low", false},
				{"direct null", `"high"`, "null", "high", false},
				{"same aliases", `"high"`, `"high"`, "high", false},
				{"conflicting aliases", `"low"`, `"high"`, "", true},
				{"invalid native with valid direct", `"none"`, `"high"`, "", true},
				{"valid native with invalid direct", `"high"`, `"none"`, "", true},
			}
			for _, value := range []string{"low", "medium", "high", "xhigh", "max", "none", "minimal", "", "HIGH", " high ", "unknown"} {
				want := value
				valid := value == "low" || value == "medium" || value == "high" || value == "xhigh"
				if route.allowMax && value == "max" {
					want, valid = "xhigh", true
				}
				for _, direct := range []bool{false, true} {
					entry := cases[0]
					entry.name = fmt.Sprintf("value=%q/direct=%t", value, direct)
					entry.want, entry.invalid = want, !valid
					if direct {
						entry.direct = fmt.Sprintf("%q", value)
					} else {
						entry.native = fmt.Sprintf("%q", value)
					}
					cases = append(cases, entry)
				}
			}
			for _, value := range []string{"1", "true", "{}", "[]"} {
				entry := cases[0]
				entry.name, entry.native, entry.invalid = "native type="+value, value, true
				cases = append(cases, entry)
				entry.name, entry.native, entry.direct = "direct type="+value, "", value
				cases = append(cases, entry)
			}
			if route.allowMax {
				entry := cases[0]
				entry.name, entry.native, entry.direct, entry.want = "equivalent max aliases", `"max"`, `"xhigh"`, "xhigh"
				cases = append(cases, entry)
				entry.name, entry.native, entry.direct = "reverse max aliases", `"xhigh"`, `"max"`
				cases = append(cases, entry)
			}
			for _, tc := range cases {
				for _, stream := range []bool{false, true} {
					for _, withTools := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/stream=%t/tools=%t", tc.name, stream, withTools), func(t *testing.T) {
							called := false
							runner = testRunner(func(model string, prompt service.Prompt, emit func(string)) (service.CompletionResult, error) {
								called = true
								if tc.invalid {
									t.Fatal("invalid effort reached the dispatcher")
								}
								if model != "claude-sonnet-5-5-thinking" {
									t.Errorf("effort changed model: %q", model)
								}
								if prompt.Effort != tc.want {
									t.Errorf("effort=%q, want %q", prompt.Effort, tc.want)
								}
								emit("<final_answer>ok</final_answer>")
								return service.CompletionResult{StatusCode: http.StatusOK}, nil
							})
							body := fmt.Sprintf(`{"model":"claude-sonnet-5-5-thinking",%s,"stream":%t`, route.input, stream)
							if tc.native != "" {
								field := fmt.Sprintf(`"%s":%s`, route.field, tc.native)
								if route.container != "" {
									field = fmt.Sprintf(`"%s":{%s}`, route.container, field)
								}
								body += "," + field
							}
							if tc.direct != "" {
								body += `,"effort":` + tc.direct
							}
							if withTools {
								body += `,"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}]`
							}
							body += "}"
							w := httptest.NewRecorder()
							c, _ := gin.CreateTestContext(w)
							c.Request = httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(body))
							c.Request.Header.Set("Content-Type", "application/json")
							route.handler(c)
							if tc.invalid {
								if w.Code != http.StatusBadRequest || called {
									t.Fatalf("invalid request: status=%d called=%t body=%s", w.Code, called, w.Body.String())
								}
								if !json.Valid(w.Body.Bytes()) || !strings.Contains(w.Body.String(), "effort") {
									t.Fatalf("expected JSON effort error before streaming: %s", w.Body.String())
								}
							} else if w.Code != http.StatusOK || !called {
								t.Fatalf("status=%d called=%t body=%s", w.Code, called, w.Body.String())
							}
						})
					}
				}
			}
		})
	}
}

func TestModelsUseUnifiedThinking(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ListModels(c)
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-sonnet-4-6", "claude-haiku-4-5-20251001", "claude-sonnet-5", "claude-sonnet-5-5"}
	if w.Code != http.StatusOK || len(response.Data) != len(want) {
		t.Fatalf("unexpected model list: %s", w.Body.String())
	}
	for i, model := range response.Data {
		if model.ID != want[i] {
			t.Errorf("model[%d]=%q, want %q", i, model.ID, want[i])
		}
	}
}
