package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const defaultManagementCompanyFundURL = "https://ew-mc.ru/funds/zakrytyy-paevoy-investitsionnyy-fond-rynochnykh-finansovykh-instrumentov-fond-pervichnykh-razmeshche/"

type Config struct {
	Debug bool

	HTTP              HTTPConfig
	DB                DBConfig
	Admin             AdminConfig
	ManagementCompany ManagementCompanyConfig
}

type HTTPConfig struct {
	Addr string
}

type DBConfig struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	Schema   string
	SSLMode  string

	URL string
}

type AdminConfig struct {
	Login        string
	PasswordHash string
}

type ManagementCompanyConfig struct {
	FundURL string
}

func Load() (*Config, error) {
	db, err := loadDBConfig()
	if err != nil {
		return nil, err
	}

	admin := AdminConfig{
		Login:        strings.TrimSpace(os.Getenv("FPR_ADMIN_LOGIN")),
		PasswordHash: strings.TrimSpace(os.Getenv("FPR_ADMIN_PASSWORD_HASH")),
	}
	if admin.Login == "" {
		return nil, fmt.Errorf("env FPR_ADMIN_LOGIN is empty")
	}
	if admin.PasswordHash == "" {
		return nil, fmt.Errorf("env FPR_ADMIN_PASSWORD_HASH is empty")
	}
	if _, errCost := bcrypt.Cost([]byte(admin.PasswordHash)); errCost != nil {
		return nil, fmt.Errorf("FPR_ADMIN_PASSWORD_HASH must be a valid bcrypt hash: %w", errCost)
	}

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":8080"
	}

	managementCompanyFundURL := os.Getenv("MANAGEMENT_COMPANY_FUND_URL")
	if managementCompanyFundURL == "" {
		managementCompanyFundURL = defaultManagementCompanyFundURL
	}

	parsedFundURL, err := url.Parse(managementCompanyFundURL)
	if err != nil {
		return nil, fmt.Errorf("parse MANAGEMENT_COMPANY_FUND_URL: %w", err)
	}
	if parsedFundURL.Host == "" || (parsedFundURL.Scheme != "http" && parsedFundURL.Scheme != "https") {
		return nil, fmt.Errorf("MANAGEMENT_COMPANY_FUND_URL must be an absolute http(s) URL")
	}

	return &Config{
		Debug:             os.Getenv("DEBUG") == "true",
		HTTP:              HTTPConfig{Addr: httpAddr},
		DB:                db,
		Admin:             admin,
		ManagementCompany: ManagementCompanyConfig{FundURL: managementCompanyFundURL},
	}, nil
}

func loadDBConfig() (DBConfig, error) {
	cfg := DBConfig{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		Name:     os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASS"),
		Schema:   os.Getenv("DB_SCHEMA"),
		SSLMode:  os.Getenv("DB_SSLMODE"),
	}

	if cfg.Host == "" {
		return cfg, fmt.Errorf("env DB_HOST is empty")
	}
	if cfg.Port == "" {
		return cfg, fmt.Errorf("env DB_PORT is empty")
	}
	if cfg.Name == "" {
		return cfg, fmt.Errorf("env DB_NAME is empty")
	}
	if cfg.User == "" {
		return cfg, fmt.Errorf("env DB_USER is empty")
	}
	if cfg.Password == "" {
		return cfg, fmt.Errorf("env DB_PASS is empty")
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}

	query := url.Values{
		"sslmode": []string{cfg.SSLMode},
	}
	if cfg.Schema != "" {
		query.Set("search_path", cfg.Schema)
	}

	dbURL := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host + ":" + cfg.Port,
		Path:     cfg.Name,
		RawQuery: query.Encode(),
	}
	cfg.URL = dbURL.String()

	return cfg, nil
}
