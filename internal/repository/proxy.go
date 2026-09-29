package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProxyInUse = errors.New("代理仍有关联账号，请先切换这些账号的代理")

type Proxy struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	URL       string    `json:"url" gorm:"uniqueIndex;not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	result := db.Model(&Proxy{}).Where("id = ?", p.ID).Updates(map[string]any{"name": p.Name, "url": p.URL})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return db.First(p, p.ID).Error
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
