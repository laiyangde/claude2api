package repository

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupProxyDB(t *testing.T) {
	t.Helper()
	previous := db
	conn, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	db = conn
	t.Cleanup(func() { _ = sqlDB.Close(); db = previous })
}

func TestProxyMigrationAndLifecycle(t *testing.T) {
	setupProxyDB(t)
	if err := db.Exec(`CREATE TABLE accounts (id integer PRIMARY KEY AUTOINCREMENT, email text NOT NULL UNIQUE, org_uuid text, cookies text, status text, created_at datetime, updated_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO accounts (email, cookies, status) VALUES ('old@example.com', '{"sessionKey":"old-key"}', 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Proxy{}, &Account{}); err != nil {
		t.Fatal(err)
	}
	old := AccountByEmail("old@example.com")
	if old == nil || old.ProxyID != nil || old.Cookies["sessionKey"] != "old-key" {
		t.Fatalf("legacy account changed: %+v", old)
	}
	p := &Proxy{Name: "exit-a", URL: "http://host:8080"}
	if err := SaveProxy(p); err != nil {
		t.Fatal(err)
	}
	if err := SetAccountProxy(old.Email, &p.ID); err != nil {
		t.Fatal(err)
	}
	loaded := AccountByEmail(old.Email)
	if loaded.Proxy == nil || loaded.Proxy.URL != p.URL {
		t.Fatal("proxy association was not loaded")
	}
	if err := DeleteProxy(p.ID); !errors.Is(err, ErrProxyInUse) {
		t.Fatalf("deleting an assigned proxy: %v", err)
	}
	p.URL = "socks5h://new-host:1080"
	if err := SaveProxy(p); err != nil {
		t.Fatal(err)
	}
	if !UpdateAccount(old.Email, func(a *Account) { a.Status = "error" }) {
		t.Fatal("status update failed")
	}
	loaded = AccountByEmail(old.Email)
	if loaded.Proxy == nil || loaded.Proxy.URL != p.URL || loaded.Status != "error" {
		t.Fatal("proxy edit or status update lost the assignment")
	}
	views, err := ListProxies()
	if err != nil || len(views) != 1 || views[0].AccountCount != 1 {
		t.Fatalf("counts: %+v, %v", views, err)
	}
	missing := p.ID + 100
	if err := SetAccountProxy(old.Email, &missing); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing proxy: %v", err)
	}
	if err := SetAccountProxy("missing", &p.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	if err := SetAccountProxy(old.Email, nil); err != nil {
		t.Fatal(err)
	}
	if AccountByEmail(old.Email).ProxyID != nil {
		t.Fatal("system fallback not saved")
	}
	if err := DeleteProxy(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProxy(p.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing proxy deletion: %v", err)
	}
}

func TestConcurrentProxyImportAndReimport(t *testing.T) {
	setupProxyDB(t)
	if err := db.AutoMigrate(&Proxy{}, &Account{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := EnsureProxy("http://host:8080", "imported"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	proxies, err := ListProxies()
	if err != nil || len(proxies) != 1 {
		t.Fatalf("expected one reused proxy: %+v, %v", proxies, err)
	}
	p := proxies[0]
	for _, id := range []*uint{&p.ID, nil, &p.ID} {
		a := &Account{Email: "user@example.com", ProxyID: id, Cookies: map[string]string{"sessionKey": "new-key"}}
		if err := UpsertAccount(a); err != nil {
			t.Fatal(err)
		}
		loaded := AccountByEmail(a.Email)
		if (loaded.ProxyID == nil) != (id == nil) || loaded.Cookies["sessionKey"] != "new-key" {
			t.Fatalf("reimport failed: %+v", loaded)
		}
	}
	if len(LoadAccounts()) != 1 {
		t.Fatal("reimport duplicated the account")
	}
	if err := SaveProxy(&Proxy{URL: p.URL}); err == nil {
		t.Fatal("duplicate address accepted")
	}
}

func TestProxyHealthMigrationAndState(t *testing.T) {
	setupProxyDB(t)
	if err := db.Exec(`CREATE TABLE proxies (id integer PRIMARY KEY AUTOINCREMENT, name text, url text NOT NULL UNIQUE, created_at datetime, updated_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO proxies (name, url) VALUES ('existing', 'http://host:8080')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Proxy{}, &Account{}); err != nil {
		t.Fatal(err)
	}
	p, err := ProxyByID(1)
	if err != nil || !p.Enabled || p.LastCheckOK != nil {
		t.Fatalf("legacy defaults: %+v %v", p, err)
	}
	if err := UpsertAccount(&Account{Email: "bound@example.com", ProxyID: &p.ID}); err != nil {
		t.Fatal(err)
	}
	check := ProxyCheckResult{CheckedAt: time.Now().UTC(), OK: true, StatusCode: 200, LatencyMS: 351, ExitIP: "203.0.113.1"}
	if err := SaveProxyCheck(p, check); err != nil {
		t.Fatal(err)
	}
	p, _ = ProxyByID(p.ID)
	if p.LastCheckOK == nil || !*p.LastCheckOK || p.ExitIP != check.ExitIP || p.LastLatencyMS != 351 {
		t.Fatalf("result not saved: %+v", p)
	}
	p.Remark = "region / note"
	if err := SaveProxy(p); err != nil {
		t.Fatal(err)
	}
	if p.Remark != "region / note" || p.LastCheckOK == nil || !*p.LastCheckOK {
		t.Fatal("remark edit invalidated health")
	}
	beforeDisable := *p
	disabled, err := SetProxyEnabled(p.ID, false)
	if err != nil || disabled.Enabled {
		t.Fatal("disable failed")
	}
	if AccountByEmail("bound@example.com").ProxyID == nil {
		t.Fatal("disable removed account binding")
	}
	if err := SetAccountProxy("bound@example.com", &p.ID); !errors.Is(err, ErrProxyDisabled) {
		t.Fatalf("disabled assignment: %v", err)
	}
	if err := UpsertAccount(&Account{Email: "new@example.com", ProxyID: &p.ID}); !errors.Is(err, ErrProxyDisabled) {
		t.Fatalf("disabled import: %v", err)
	}
	if err := SaveProxyCheck(&beforeDisable, check); !errors.Is(err, ErrProxyChanged) {
		t.Fatalf("stale check after disable: %v", err)
	}
	p, err = SetProxyEnabled(p.ID, true)
	if err != nil || p.LastCheckedAt != nil || p.LastCheckOK != nil || p.ExitIP != "" {
		t.Fatal("reactivation kept stale health")
	}
	beforeEdit := *p
	p.URL = "http://other:8080"
	if err := SaveProxy(p); err != nil {
		t.Fatal(err)
	}
	if err := SaveProxyCheck(&beforeEdit, check); !errors.Is(err, ErrProxyChanged) {
		t.Fatal("old address check accepted")
	}
	check.OK, check.ExitIP, check.Error = false, "", "timeout"
	if err := SaveProxyCheck(p, check); err != nil {
		t.Fatal(err)
	}
	p, _ = ProxyByID(p.ID)
	if p.LastCheckOK == nil || *p.LastCheckOK || p.LastCheckError != "timeout" {
		t.Fatal("failed check not saved")
	}
}
