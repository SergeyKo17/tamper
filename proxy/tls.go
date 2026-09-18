package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// serverCredentials builds the credentials the proxy presents to gRPC clients.
// A missing configuration serves plaintext.
func serverCredentials(cfg *config.ServerTLS) (credentials.TransportCredentials, error) {
	if cfg == nil {
		return insecure.NewCredentials(), nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load listen certificate: %w", err)
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}), nil
}

// clientCredentials builds the credentials the proxy uses to dial the target.
func clientTLSConfig(cfg *config.ClientTLS) (*tls.Config, error) {
	if cfg == nil {
		return nil, nil
	}

	if cfg.InsecureSkipVerify {
		slog.Warn("target certificate verification is disabled")
	}

	tlsCfg := &tls.Config{
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // Opt-in, for self-signed targets.
		MinVersion:         tls.VersionTLS12,
	}

	if cfg.CAFile != "" {
		pool, err := certPool(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.RootCAs = pool
	}

	// Validation guarantees the key file is set alongside the certificate.
	if cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load target client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// certPool reads a PEM bundle into a pool of trusted certificates.
func certPool(path string) (*x509.CertPool, error) {
	//nolint:gosec
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ca file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("ca file holds no certificates")
	}
	return pool, nil
}

// checkTarget completes one TLS handshake with the target before the proxy
// starts serving. A rejected certificate is a configuration mistake and stops
// startup; any other failure is only reported, because the target is allowed
// to come up later than the proxy.
func checkTarget(ctx context.Context, addr string, cfg *tls.Config) error {
	if cfg == nil {
		return nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	dialer := &tls.Dialer{Config: cfg}
	conn, err := dialer.DialContext(checkCtx, "tcp", addr)
	if err != nil {
		var cve *tls.CertificateVerificationError
		if errors.As(err, &cve) {
			return fmt.Errorf("verify target certificate: %w", err)
		}
		slog.Warn("target is not reachable yet", "addr", addr, "err", err)
		return nil
	}

	if err := conn.Close(); err != nil {
		slog.Error("close preflight connection", "err", err)
	}
	return nil
}
