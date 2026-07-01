package qwkapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// loadOrCreateCert resolves the API's TLS certificate. If cfg.CertFile and
// cfg.KeyFile are both set they are loaded; otherwise a self-signed ECDSA cert
// is generated at dir/qwkapi_{cert,key}.pem (created once, then reused). It
// returns the certificate and its SHA-256 fingerprint (hex, colon-separated).
func loadOrCreateCert(cfg config.QWKAPIConfig, dir string) (tls.Certificate, string, error) {
	certPath, keyPath := cfg.CertFile, cfg.KeyFile
	switch {
	case certPath != "" && keyPath != "":
		// Explicit cert/key pair — use as configured.
	case certPath == "" && keyPath == "":
		// Neither set — use the auto-managed self-signed pair.
		certPath = filepath.Join(dir, "qwkapi_cert.pem")
		keyPath = filepath.Join(dir, "qwkapi_key.pem")
		if !fileExists(certPath) || !fileExists(keyPath) {
			if err := generateSelfSigned(cfg.Host, certPath, keyPath); err != nil {
				return tls.Certificate{}, "", fmt.Errorf("generate self-signed cert: %w", err)
			}
		}
	default:
		// Exactly one set — a misconfiguration; fail closed rather than silently
		// ignoring the configured file.
		return tls.Certificate{}, "", fmt.Errorf("qwkapi: certFile and keyFile must be set together (certFile=%q keyFile=%q)", cfg.CertFile, cfg.KeyFile)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("load cert: %w", err)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return cert, fingerprintHex(sum[:]), nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func fingerprintHex(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, ":")
}

func generateSelfSigned(host, certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "ViSiON/3 QWK API"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if host != "" && host != "0.0.0.0" {
		if ip := net.ParseIP(host); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}
