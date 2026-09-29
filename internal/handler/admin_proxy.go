package handler

import (
	"errors"
	"net/http"
	"strings"

	"claude2api/internal/repository"
	"claude2api/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func AdminListProxies(c *gin.Context) {
	proxies, err := repository.ListProxies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载代理失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"proxies": proxies})
}

func AdminSaveProxy(c *gin.Context) {
	var body struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	address, err := service.NormalizeProxyURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = service.ProxyDisplayURL(address)
	}
	p := repository.Proxy{ID: body.ID, Name: name, URL: address}
	if err := repository.SaveProxy(&p); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "保存代理失败，地址可能已存在"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"proxy": p})
}

func AdminDeleteProxy(c *gin.Context) {
	var body struct {
		ID uint `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择代理"})
		return
	}
	if err := repository.DeleteProxy(body.ID); err != nil {
		switch {
		case errors.Is(err, repository.ErrProxyInUse):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "删除代理失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": 1})
}

func AdminSetAccountProxy(c *gin.Context) {
	var body struct {
		Email   string `json:"email"`
		ProxyID *uint  `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Email) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if body.ProxyID != nil && *body.ProxyID == 0 {
		body.ProxyID = nil
	}
	email := strings.TrimSpace(body.Email)
	if err := repository.SetAccountProxy(email, body.ProxyID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号或代理不存在，请刷新后重试"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "切换代理失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": service.PublicAccountView(repository.AccountByEmail(email))})
}
