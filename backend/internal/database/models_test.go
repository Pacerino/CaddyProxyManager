package database

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestJSONValue(t *testing.T) {
	// Empty -> "null".
	v, err := JSON(nil).Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != "null" {
		t.Errorf("empty Value() = %v, want null", v)
	}

	v, err = JSON(`{"a":1}`).Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != `{"a":1}` {
		t.Errorf("Value() = %v", v)
	}
}

func TestJSONScan(t *testing.T) {
	var j JSON
	if err := j.Scan([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if string(j) != `{"a":1}` {
		t.Errorf("Scan bytes = %s", j)
	}

	if err := j.Scan(`{"b":2}`); err != nil {
		t.Fatal(err)
	}
	if string(j) != `{"b":2}` {
		t.Errorf("Scan string = %s", j)
	}

	if err := j.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if string(j) != "null" {
		t.Errorf("Scan nil = %s, want null", j)
	}

	if err := j.Scan(12345); err == nil {
		t.Error("expected error scanning unsupported type")
	}
}

func TestJSONMarshalUnmarshal(t *testing.T) {
	// Marshalling embeds raw JSON rather than a base64 byte array.
	type wrapper struct {
		Data JSON `json:"data"`
	}
	out, err := json.Marshal(wrapper{Data: JSON(`{"x":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"data":{"x":true}}` {
		t.Errorf("marshal = %s", out)
	}

	// Empty marshals to null.
	out, _ = json.Marshal(wrapper{})
	if string(out) != `{"data":null}` {
		t.Errorf("empty marshal = %s", out)
	}

	var w wrapper
	if err := json.Unmarshal([]byte(`{"data":{"y":5}}`), &w); err != nil {
		t.Fatal(err)
	}
	if string(w.Data) != `{"y":5}` {
		t.Errorf("unmarshal captured %s", w.Data)
	}
}

func newModelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&Host{}, &Upstream{}, &HostPlugin{}, &DNSProvider{}, &Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestDNSProvider_NameUnique(t *testing.T) {
	db := newModelsTestDB(t)
	first := DNSProvider{Name: "prod", ModuleID: "dns.providers.cloudflare", Config: JSON(`{"api_token":"x"}`)}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	second := DNSProvider{Name: "prod", ModuleID: "dns.providers.route53", Config: JSON(`{}`)}
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("second insert with the same name should fail")
	}
}

func TestHost_DNSProviderIDNullable(t *testing.T) {
	db := newModelsTestDB(t)
	host := Host{Domains: "example.com"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	reload := func() Host {
		var h Host
		if err := db.First(&h, host.ID).Error; err != nil {
			t.Fatal(err)
		}
		return h
	}
	if got := reload(); got.DNSProviderID != nil {
		t.Fatalf("new host DNSProviderID = %v, want nil", *got.DNSProviderID)
	}

	id := uint(7)
	host.DNSProviderID = &id
	if err := db.Save(&host).Error; err != nil {
		t.Fatal(err)
	}
	if got := reload(); got.DNSProviderID == nil || *got.DNSProviderID != 7 {
		t.Fatalf("DNSProviderID = %v, want 7", got.DNSProviderID)
	}

	host.DNSProviderID = nil
	if err := db.Save(&host).Error; err != nil {
		t.Fatal(err)
	}
	if got := reload(); got.DNSProviderID != nil {
		t.Fatalf("cleared DNSProviderID = %v, want nil", *got.DNSProviderID)
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		"dns.providers.cloudflare": "cloudflare",
		"cloudflare":               "cloudflare",
		"":                         "",
	}
	for in, want := range cases {
		if got := ShortName(in); got != want {
			t.Errorf("ShortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSettings(t *testing.T) {
	db := newModelsTestDB(t)
	if got := GetSetting(db, "plugins.autoUpdate", "false"); got != "false" {
		t.Fatalf("default: %q", got)
	}
	if err := SetSetting(db, "plugins.autoUpdate", "true"); err != nil {
		t.Fatal(err)
	}
	if got := GetSetting(db, "plugins.autoUpdate", "false"); got != "true" {
		t.Fatalf("after set: %q", got)
	}
	// Overwrite an existing key.
	if err := SetSetting(db, "plugins.autoUpdate", "false"); err != nil {
		t.Fatal(err)
	}
	if got := GetSetting(db, "plugins.autoUpdate", "true"); got != "false" {
		t.Fatalf("after overwrite: %q", got)
	}
	var n int64
	db.Model(&Setting{}).Count(&n)
	if n != 1 {
		t.Fatalf("rows: %d", n)
	}
}
