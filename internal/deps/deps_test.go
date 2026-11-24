package deps

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http"
	"testing"
	"time"
)

func TestMOEXClientWithNoConfiguredCA(t *testing.T) {
	client, err := newMOEXClient("")
	if err != nil {
		t.Fatal(err)
	}
	verify := client.Transport.(*http.Transport).TLSClientConfig.VerifyConnection
	if verify == nil {
		t.Fatal("missing TLS peer verification")
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, test := range []struct {
		name       string
		serverName string
		dnsName    string
		expires    time.Time
		usage      x509.ExtKeyUsage
		wantError  bool
	}{
		{"unknown issuer", "iss.moex.com", "iss.moex.com", now.Add(time.Hour), x509.ExtKeyUsageServerAuth, false},
		{"different endpoint", "other.example", "other.example", now.Add(time.Hour), x509.ExtKeyUsageServerAuth, true},
		{"different certificate name", "iss.moex.com", "other.example", now.Add(time.Hour), x509.ExtKeyUsageServerAuth, true},
		{"expired certificate", "iss.moex.com", "iss.moex.com", now.Add(-time.Hour), x509.ExtKeyUsageServerAuth, true},
		{"client-only certificate", "iss.moex.com", "iss.moex.com", now.Add(time.Hour), x509.ExtKeyUsageClientAuth, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cert := &x509.Certificate{
				SerialNumber: big.NewInt(1),
				DNSNames:     []string{test.dnsName},
				NotBefore:    now.Add(-2 * time.Hour),
				NotAfter:     test.expires,
				ExtKeyUsage:  []x509.ExtKeyUsage{test.usage},
			}
			der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			err = verify(tls.ConnectionState{
				ServerName:       test.serverName,
				PeerCertificates: []*x509.Certificate{peer},
			})
			if (err != nil) != test.wantError {
				t.Fatalf("TLS verification error = %v, want error = %t", err, test.wantError)
			}
		})
	}
}
