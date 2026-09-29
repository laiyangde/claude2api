package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProxyInUse = errors.New("代理仍有关联账号，请先切换这些账号的代理")
var ErrProxyDisabled = errors.New("代理已停用，请先启用或选择其他代理")
var ErrProxyChanged = errors.New("代理配置已变化，请重新检测")

type Proxy struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	Name           string     `json:"name"`
	URL            string     `json:"url" gorm:"uniqueIndex;not null"`
	Remark         string     `json:"remark"`
	Enabled        bool       `json:"enabled" gorm:"not null;default:true"`
	CheckRevision  uint64     `json:"-" gorm:"not null;default:0"`
	LastCheckedAt  *time.Time `json:"last_checked_at"`
	LastCheckOK    *bool      `json:"last_check_ok"`
	LastStatusCode int        `json:"last_status_code"`
	LastLatencyMS  int64      `json:"last_latency_ms"`
	ExitIP         string     `json:"exit_ip"`
	LastCheckError string     `json:"last_check_error"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ProxyView struct {
	Proxy
	AccountCount int64 `json:"account_count"`
}

func ListProxies() ([]ProxyView, error) {
	out := []ProxyView{}
	err := db.Model(&Proxy{}).Select("proxies.*, (SELECT COUNT(*) FROM accounts WHERE accounts.proxy_id = proxies.id) AS account_count").Order("proxies.id ASC").Scan(&out).Error
	return out, err
}

func SaveProxy(p *Proxy) error {
	if p.ID == 0 {
		return db.Create(p).Error
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var old Proxy
		if err := tx.First(&old, p.ID).Error; err != nil {
			return err
		}
		updates := map[string]any{"name": p.Name, "url": p.URL, "remark": p.Remark}
		if old.URL != p.URL {
			clearProxyCheck(updates)
		}
		if err := tx.Model(&old).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(p, p.ID).Error
	})
}

func ProxyByID(id uint) (*Proxy, error) {
	var p Proxy
	err := db.First(&p, id).Error
	return &p, err
}

/** A new address or reactivation must not reuse the old exit's health result. */
func clearProxyCheck(updates map[string]any) {
	updates["check_revision"] = gorm.Expr("check_revision + 1")
	updates["last_checked_at"], updates["last_check_ok"] = nil, nil
	updates["last_status_code"], updates["last_latency_ms"] = 0, 0
	updates["exit_ip"], updates["last_check_error"] = "", ""
}

func SetProxyEnabled(id uint, enabled bool) (*Proxy, error) {
	err := db.Transaction(func(tx *gorm.DB) error {
		var p Proxy
		if err := tx.First(&p, id).Error; err != nil {
			return err
		}
		if p.Enabled == enabled {
			return nil
		}
		updates := map[string]any{"enabled": enabled, "check_revision": gorm.Expr("check_revision + 1")}
		if enabled {
			clearProxyCheck(updates)
		}
		return tx.Model(&p).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return ProxyByID(id)
}

type ProxyCheckResult struct {
	CheckedAt  time.Time
	OK         bool
	StatusCode int
	LatencyMS  int64
	ExitIP     string
	Error      string
}

/** Discard checks for an address that was edited or toggled while in flight. */
func SaveProxyCheck(p *Proxy, check ProxyCheckResult) error {
	result := db.Model(&Proxy{}).Where("id = ? AND url = ? AND check_revision = ?", p.ID, p.URL, p.CheckRevision).
		Updates(map[string]any{"last_checked_at": check.CheckedAt, "last_check_ok": check.OK,
			"last_status_code": check.StatusCode, "last_latency_ms": check.LatencyMS,
			"exit_ip": check.ExitIP, "last_check_error": check.Error})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrProxyChanged
	}
	return nil
}

/** Importing the same address concurrently reuses one managed proxy. */
func EnsureProxy(address, name string) (*Proxy, error) {
	p := Proxy{URL: address, Name: name}
	err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "url"}}, DoNothing: true}).Create(&p).Error
	if err != nil {
		return nil, err
	}
	var stored Proxy
	err = db.Where("url = ?", address).First(&stored).Error
	return &stored, err
}

/** Refuse deletion while accounts depend on this exit address. */
func DeleteProxy(id uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Account{}).Where("proxy_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrProxyInUse
		}
		result := tx.Delete(&Proxy{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func SetAccountProxy(email string, proxyID *uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if proxyID != nil {
			var p Proxy
			if err := tx.First(&p, *proxyID).Error; err != nil {
				return err
			}
			if !p.Enabled {
				return ErrProxyDisabled
			}
		}
		result := tx.Model(&Account{}).Where("email = ?", email).Update("proxy_id", proxyID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
