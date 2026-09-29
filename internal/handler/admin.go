package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"claude2api/internal/config"
	"claude2api/internal/repository"
	"claude2api/internal/service"

	"github.com/gin-gonic/gin"
)

func AdminAccounts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"accounts": service.PublicAccounts()})
}

// AdminImportAccounts 批量导入账号。
func AdminImportAccounts(c *gin.Context) {
	var body struct {
		SessionKeys string `json:"session_keys"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}

	items, err := parseImportSessionKeys(body.SessionKeys)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可导入的 sessionKey"})
		return
	}
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	send := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b)
		c.Writer.Flush()
	}
	imported, failed := 0, 0
	send("progress", gin.H{"total": len(items), "completed": 0, "imported": 0, "failed": 0})
	for result := range importAccounts(items) {
		if result.err != "" {
			failed++
		} else {
			imported++
		}
		send("progress", gin.H{"total": len(items), "completed": imported + failed, "imported": imported, "failed": failed, "email": result.email, "error": result.err, "line": result.line})
	}
	send("done", gin.H{"done": true, "total": len(items), "imported": imported, "failed": failed})
	slog.Info("[导入] 批量导入完成", "total", len(items), "imported", imported, "failed", failed)
}

type importItem struct {
	sessionKey string
	proxy      string
	line       int
}

func parseImportSessionKeys(text string) ([]importItem, error) {
	seen := map[string]string{}
	items := []importItem{}
	for i, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) > 2 {
			return nil, fmt.Errorf("第 %d 行格式错误：每行填写 sessionKey，空格后可选填代理地址", i+1)
		}
		item := importItem{sessionKey: fields[0], line: i + 1}
		if len(fields) == 2 {
			proxy, err := service.NormalizeProxyURL(fields[1])
			if err != nil {
				return nil, fmt.Errorf("第 %d 行：%w", i+1, err)
			}
			item.proxy = proxy
		}
		if previous, exists := seen[item.sessionKey]; exists {
			if previous != item.proxy {
				return nil, fmt.Errorf("第 %d 行的 sessionKey 重复且代理不同，请保留一条", i+1)
			}
			continue
		}
		seen[item.sessionKey] = item.proxy
		items = append(items, item)
	}
	return items, nil
}

type importResult struct {
	email, err string
	line       int
}

func importAccounts(items []importItem) <-chan importResult {
	results := make(chan importResult, len(items))
	go func() {
		defer close(results)
		var wg sync.WaitGroup
		// ponytail: 固定 10 并发，上游限流变化时再改为配置项。
		sem := make(chan struct{}, 10)
		slog.Info("[导入] 批量导入开始", "total", len(items))
		for _, item := range items {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				key, proxy := item.sessionKey, item.proxy
				account := &repository.Account{Cookies: map[string]string{"sessionKey": key}, Status: "active"}
				if proxy == "" {
					proxy = config.Get().Proxy
				} else {
					p, err := repository.EnsureProxy(proxy, service.ProxyDisplayURL(proxy))
					if err != nil {
						results <- importResult{err: "保存代理失败", line: item.line}
						return
					}
					account.ProxyID, account.Proxy = &p.ID, p
					proxy, err = service.EnsureAccountProxy(context.Background(), account)
					if err != nil {
						results <- importResult{err: err.Error(), line: item.line}
						return
					}
				}
				client := service.NewClaudeAI(key, proxy, key)
				info, err := client.GetUserInfo()
				if err != nil || info == nil || info.Email == "" {
					slog.Warn("[导入] 查询账号信息失败", "err", err)
					message := "查询账号信息失败"
					if err != nil && strings.Contains(err.Error(), "account_session_invalid") {
						message = "sessionKey 已失效或格式错误"
					}
					results <- importResult{err: message, line: item.line}
					return
				}
				account.Email, account.OrgUUID = info.Email, info.OrgUUID
				if err := repository.UpsertAccount(account); err != nil {
					slog.Warn("[导入] 保存账号失败", "email", info.Email, "err", err)
					results <- importResult{email: info.Email, err: "保存账号失败", line: item.line}
					return
				}
				results <- importResult{email: info.Email, line: item.line}
			})
		}
		wg.Wait()
	}()
	return results
}

// AdminRefreshAccount 刷新账号。
func AdminRefreshAccount(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	_ = c.ShouldBindJSON(&body)
	account, removed, err := service.RefreshAccount(strings.TrimSpace(body.Email))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if removed {
		c.JSON(http.StatusOK, gin.H{"removed": 1})
		return
	}
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在或缺少 sessionKey"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": service.PublicAccountView(account)})
}

func AdminGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"config": config.Get()})
}

func AdminUpdateConfig(c *gin.Context) {
	var patch map[string]any
	_ = c.ShouldBindJSON(&patch)
	cfg := config.Update(patch)
	slog.Info("[配置] 已更新配置项", "keys", configKeys(patch))
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

func AdminDeleteAccount(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	_ = c.ShouldBindJSON(&body)
	removed := repository.DeleteAccount(body.Email)
	slog.Info("[删除] 账号已删除", "email", body.Email, "removed", removed)
	c.JSON(http.StatusOK, gin.H{"removed": removed})
}

func AdminDeleteExpired(c *gin.Context) {
	emails := repository.DeleteAccountsByStatus([]string{"expired"})
	slog.Info("[删除] 一键删除失效账号", "count", len(emails))
	c.JSON(http.StatusOK, gin.H{"removed": len(emails), "emails": emails})
}

func AdminListKeys(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"keys": repository.ListAPIKeys()})
}

func AdminCreateKey(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	}
	_ = c.ShouldBindJSON(&body)
	k, err := repository.CreateAPIKey(strings.TrimSpace(body.Name), strings.TrimSpace(body.Key))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Info("[密钥] 新建 API 密钥", "name", k.Name)
	c.JSON(http.StatusOK, gin.H{"key": k})
}

func AdminDeleteKey(c *gin.Context) {
	var body struct {
		ID int64 `json:"id"`
	}
	_ = c.ShouldBindJSON(&body)
	removed := repository.DeleteAPIKey(body.ID)
	slog.Info("[密钥] 删除 API 密钥", "id", body.ID, "removed", removed)
	c.JSON(http.StatusOK, gin.H{"removed": removed})
}

func AdminListLogs(c *gin.Context) {
	limit := atoiDefault(c.Query("limit"), 50)
	offset := atoiDefault(c.Query("offset"), 0)
	c.JSON(http.StatusOK, gin.H{
		"logs":  repository.ListAPILogs(limit, offset),
		"total": repository.CountAPILogs(),
	})
}

func AdminGetLog(c *gin.Context) {
	id := int64(atoiDefault(c.Param("id"), 0))
	log, ok := repository.GetAPILog(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "日志不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"log": log})
}

func AdminDeleteLogs(c *gin.Context) {
	var body struct {
		IDs  []int64 `json:"ids"`
		Keep *int    `json:"keep"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Keep != nil {
		if *body.Keep < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "保留条数不能小于 0"})
			return
		}
		removed := repository.TrimAPILogs(*body.Keep)
		slog.Info("[日志] 清理调用日志", "keep", *body.Keep, "removed", removed)
		c.JSON(http.StatusOK, gin.H{"removed": removed})
		return
	}
	if len(body.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的日志"})
		return
	}
	removed := repository.DeleteAPILogs(body.IDs)
	slog.Info("[日志] 批量删除调用日志", "removed", removed)
	c.JSON(http.StatusOK, gin.H{"removed": removed})
}

// configKeys 只记录配置键名。
func configKeys(patch map[string]any) []string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	return keys
}

// atoiDefault 解析整数。
func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
