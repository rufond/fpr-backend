package deps

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/config"
	"github.com/rufond/fpr-backend/internal/providers/managementcompany"
	"github.com/rufond/fpr-backend/internal/providers/moex"
	"github.com/rufond/fpr-backend/internal/providers/yahoo"
)

type Deps struct {
	Config *config.Config
	DB     *pgxpool.Pool
	Logger zerolog.Logger

	ManagementCompany *managementcompany.Provider
	MOEX              *moex.Provider
	Yahoo             *yahoo.Provider
}

func New(ctx context.Context, cfg *config.Config) (*Deps, error) {
	logger := newLogger(cfg)
	moexClient, err := newMOEXClient(cfg.MOEXCAFile)
	if err != nil {
		return nil, err
	}

	db, err := newDB(ctx, cfg)
	if err != nil {
		return nil, err
	}

	yahooProvider, errYahoo := yahoo.NewProvider(yahoo.ProxyConfig{
		Mode:    cfg.Yahoo.ProxyMode,
		URL:     cfg.Yahoo.ProxyURL,
		ListURL: cfg.Yahoo.ProxyListURL,
	})
	if errYahoo != nil {
		db.Close()
		return nil, errYahoo
	}

	return &Deps{
		Config:            cfg,
		DB:                db,
		Logger:            logger,
		ManagementCompany: managementcompany.NewProvider(cfg.ManagementCompany.FundURL, nil),
		MOEX:              moex.NewProvider("", moexClient),
		Yahoo:             yahooProvider,
	}, nil
}

func (d *Deps) Close() {
	if d == nil {
		return
	}
	if d.Yahoo != nil {
		d.Yahoo.Close()
	}
	if d.DB != nil {
		d.DB.Close()
	}
}

func newLogger(cfg *config.Config) zerolog.Logger {
	writer := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339Nano,
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	logger := log.Output(writer)
	if cfg.Debug {
		logger = logger.With().Caller().Logger()
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = logger

	return logger
}

func newDB(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DB.URL)
	if err != nil {
		return nil, fmt.Errorf("pg connection: %w", err)
	}

	if errPool := pool.Ping(ctx); errPool != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", errPool)
	}

	return pool, nil
}

func newMOEXClient(path string) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if path == "" {
		// VerifyConnection checks the ISS peer and permits only an unknown issuer.
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = verifyMOEXConnection
	} else {
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read MOEX CA bundle: %w", err)
		}

		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool for MOEX: %w", err)
		}

		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("MOEX_CA_FILE contains no valid PEM certificates")
		}
		tlsConfig.RootCAs = roots
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{
		Timeout:   moex.DefaultRequestTimeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 || request.URL.Hostname() != via[0].URL.Hostname() || request.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}, nil
}

func verifyMOEXConnection(state tls.ConnectionState) error {
	if state.ServerName != "iss.moex.com" || len(state.PeerCertificates) == 0 {
		return fmt.Errorf("expected a TLS certificate for iss.moex.com")
	}

	options := x509.VerifyOptions{
		DNSName:       state.ServerName,
		Intermediates: x509.NewCertPool(),
	}
	for _, certificate := range state.PeerCertificates[1:] {
		options.Intermediates.AddCert(certificate)
	}
	certificate := state.PeerCertificates[0]
	_, err := certificate.Verify(options)
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); !ok {
		return err
	}

	// Trust the presented leaf for this verification only. The usual name,
	// validity and server-auth checks still apply, but its issuer is unverified.
	options.Roots = x509.NewCertPool()
	options.Roots.AddCert(certificate)
	_, err = certificate.Verify(options)
	return err
}
