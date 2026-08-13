package config

import (
	"net/url"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	setRequiredDBEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP addr = %q, want :8080", cfg.HTTP.Addr)
	}
	if cfg.ManagementCompany.FundURL != defaultManagementCompanyFundURL {
		t.Fatalf("management company fund URL = %q, want default", cfg.ManagementCompany.FundURL)
	}
	if cfg.DB.SSLMode != "disable" {
		t.Fatalf("DB sslmode = %q, want disable", cfg.DB.SSLMode)
	}
}

func TestLoadBuildsEscapedDatabaseURL(t *testing.T) {
	setRequiredDBEnv(t)
	t.Setenv("DB_USER", "fpr user")
	t.Setenv("DB_PASS", "p@ss:/word")
	t.Setenv("DB_SCHEMA", "fpr_runtime")
	t.Setenv("DB_SSLMODE", "require")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	parsed, err := url.Parse(cfg.DB.URL)
	if err != nil {
		t.Fatalf("parse DB URL: %v", err)
	}
	if parsed.User.Username() != "fpr user" {
		t.Fatalf("DB username = %q", parsed.User.Username())
	}
	password, _ := parsed.User.Password()
	if password != "p@ss:/word" {
		t.Fatalf("DB password = %q", password)
	}
	if parsed.Query().Get("sslmode") != "require" || parsed.Query().Get("search_path") != "fpr_runtime" {
		t.Fatalf("DB query = %q", parsed.RawQuery)
	}
}

func TestLoadRequiresDatabasePassword(t *testing.T) {
	setRequiredDBEnv(t)
	t.Setenv("DB_PASS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want DB_PASS error")
	}
}

func TestLoadRejectsInvalidManagementCompanyURL(t *testing.T) {
	setRequiredDBEnv(t)
	t.Setenv("MANAGEMENT_COMPANY_FUND_URL", "://broken")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want MANAGEMENT_COMPANY_FUND_URL error")
	}
}

func setRequiredDBEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DEBUG", "false")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "fpr")
	t.Setenv("DB_USER", "fpr")
	t.Setenv("DB_PASS", "secret")
	t.Setenv("DB_SCHEMA", "")
	t.Setenv("DB_SSLMODE", "")
	t.Setenv("MANAGEMENT_COMPANY_FUND_URL", "")
}
