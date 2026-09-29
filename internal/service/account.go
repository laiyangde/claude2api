package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"claude2api/internal/config"
	"claude2api/internal/repository"
)

// PublicAccount 是前端账号视图。
type PublicAccount struct {
	Email        string    `json:"email"`
	OrgUUID      string    `json:"org_uuid"`
	Status       string    `json:"status,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HasSession   bool      `json:"has_session"`
	ProxyID      *uint     `json:"proxy_id"`
	ProxyName    string    `json:"proxy_name"`
	ProxyURL     string    `json:"proxy_url"`
	ProxyEnabled *bool     `json:"proxy_enabled"`
	ProxyCheckOK *bool     `json:"proxy_check_ok"`
}

func SessionKey(account *repository.Account) string {
	if account == nil || account.Cookies == nil {
		return ""
	}
	return account.Cookies["sessionKey"]
}

func AccountUsable(account *repository.Account) bool {
	if SessionKey(account) == "" || account.Status == "expired" {
		return false
	}
	if account.ProxyID == nil {
		return true
	}
	p := account.Proxy
	return p != nil && p.Enabled && (!ProxyCheckFresh(p) || p.LastCheckOK == nil || *p.LastCheckOK)
}

func AccountByEmail(email string) *repository.Account {
	account := repository.AccountByEmail(email)
	if !AccountUsable(account) {
		return nil
	}
	return account
}

func PublicAccountView(account *repository.Account) *PublicAccount {
	if account == nil {
		return nil
	}
	view := &PublicAccount{
		Email:      account.Email,
		OrgUUID:    account.OrgUUID,
		Status:     account.Status,
		CreatedAt:  account.CreatedAt,
		UpdatedAt:  account.UpdatedAt,
		HasSession: SessionKey(account) != "",
		ProxyID:    account.ProxyID,
	}
	if account.Proxy != nil {
		view.ProxyName = account.Proxy.Name
		view.ProxyURL = ProxyDisplayURL(account.Proxy.URL)
		view.ProxyEnabled = &account.Proxy.Enabled
		view.ProxyCheckOK = account.Proxy.LastCheckOK
	}
	return view
}

// PublicAccounts 返回前端账号列表。
func PublicAccounts() []PublicAccount {
	src := repository.LoadAccounts()
	out := make([]PublicAccount, 0, len(src))
	for i := range src {
		out = append(out, *PublicAccountView(&src[i]))
	}
	return out
}

func StartAccountStatusMonitor() {
	go func() {
		for {
			time.Sleep(time.Duration(config.Get().StatusCheckIntervalSeconds) * time.Second)
			checkAccountStatuses()
		}
	}()
}

func RefreshAccount(email string) (*repository.Account, bool, error) {
	account := repository.AccountByEmail(email)
	sessionKey := SessionKey(account)
	if sessionKey == "" {
		return nil, false, nil
	}

	proxy, err := EnsureAccountProxy(context.Background(), account)
	if err != nil {
		return account, false, err
	}
	client := NewClaudeAI(sessionKey, proxy, email)
	info, err := client.GetUserInfo()
	if repository.AccountByEmail(account.Email) == nil {
		return nil, false, nil
	}
	if err != nil || info == nil || info.Email == "" {
		if err != nil && strings.Contains(err.Error(), "account_session_invalid") {
			if config.Get().RemoveInvalidAccount {
				repository.DeleteAccount(account.Email)
				slog.Warn("[账号刷新] 会话失效，已移除账号", "email", account.Email)
				return nil, true, nil
			}
			repository.UpdateAccount(account.Email, func(a *repository.Account) { a.Status = "expired" })
			return repository.AccountByEmail(account.Email), false, nil
		}
		repository.UpdateAccount(account.Email, func(a *repository.Account) { a.Status = "error" })
		slog.Warn("[账号刷新] 查询失败，保留账号", "email", account.Email, "err", err)
		return repository.AccountByEmail(account.Email), false, nil
	}

	repository.UpdateAccount(account.Email, func(a *repository.Account) {
		a.Email, a.OrgUUID, a.Status = info.Email, info.OrgUUID, "active"
	})
	return repository.AccountByEmail(info.Email), false, nil
}

func checkAccountStatuses() {
	for _, account := range repository.LoadAccounts() {
		if SessionKey(&account) == "" {
			continue
		}
		if _, _, err := RefreshAccount(account.Email); err != nil {
			slog.Warn("[账号巡检] 跳过不可用代理", "email", account.Email, "err", err)
		}
	}
}
