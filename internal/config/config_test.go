package config

import (
	"net/url"
	"testing"
)

const testAdminPasswordHash = "$2a$10$qy9oAvf/CE8rc5ANmO0v5O/hF82llpqPEMRLqx7n6SlT.v.X.A8ru"

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP addr = %q, want :8080", cfg.HTTP.Addr)
	}
	if cfg.Admin.Login != "admin" || cfg.Admin.PasswordHash != testAdminPasswordHash {
		t.Fatalf("Admin = %#v", cfg.Admin)
	}
	if cfg.ManagementCompany.FundURL != defaultManagementCompanyFundURL {
		t.Fatalf("management company fund URL = %q, want default", cfg.ManagementCompany.FundURL)
	}
	if cfg.DB.SSLMode != "disable" {
		t.Fatalf("DB sslmode = %q, want disable", cfg.DB.SSLMode)
	}
}

func TestLoadBuildsEscapedDatabaseURL(t *testing.T) {
	setRequiredEnv(t)
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
	setRequiredEnv(t)
	t.Setenv("DB_PASS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want DB_PASS error")
	}
}

func TestLoadRequiresAdminCredentials(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
	}{
		{name: "login", env: "FPR_ADMIN_LOGIN"},
		{name: "password hash", env: "FPR_ADMIN_PASSWORD_HASH"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(test.env, "")

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want %s error", test.env)
			}
		})
	}
}

func TestLoadRejectsInvalidAdminPasswordHash(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FPR_ADMIN_PASSWORD_HASH", "not-a-bcrypt-hash")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want bcrypt hash error")
	}
}

func TestLoadRejectsInvalidManagementCompanyURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MANAGEMENT_COMPANY_FUND_URL", "://broken")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want MANAGEMENT_COMPANY_FUND_URL error")
	}
}

func setRequiredEnv(t *testing.T) {
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
	t.Setenv("FPR_ADMIN_LOGIN", "admin")
	t.Setenv("FPR_ADMIN_PASSWORD_HASH", testAdminPasswordHash)
	t.Setenv("MANAGEMENT_COMPANY_FUND_URL", "")
}
