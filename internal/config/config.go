package config

import (
	"fmt"
	"net/url"
	"os"
)

const defaultManagementCompanyFundURL = "https://ew-mc.ru/funds/zakrytyy-paevoy-investitsionnyy-fond-rynochnykh-finansovykh-instrumentov-fond-pervichnykh-razmeshche/"

type Config struct {
	Debug bool

	HTTP              HTTPConfig
	DB                DBConfig
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

type ManagementCompanyConfig struct {
	FundURL string
}

func Load() (*Config, error) {
	db, err := loadDBConfig()
	if err != nil {
		return nil, err
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
