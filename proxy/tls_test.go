package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// testCA is a throwaway certificate authority backed by files in t.TempDir.
type testCA struct {
	dir      string
	certPath string
	cert     *x509.Certificate
	key      *ecdsa.PrivateKey
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tamper test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	ca := &testCA{dir: t.TempDir(), cert: cert, key: key}
	ca.certPath = filepath.Join(ca.dir, "ca.crt")
	writePEM(t, ca.certPath, "CERTIFICATE", der)
	return ca
}

// issue signs a server certificate valid for localhost and 127.0.0.1.
func (ca *testCA) issue(t *testing.T, name string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certPath = filepath.Join(ca.dir, name+".crt")
	keyPath = filepath.Join(ca.dir, name+".key")
	writePEM(t, certPath, "CERTIFICATE", der)
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)
	return certPath, keyPath
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatal(err)
	}
}

// startTLSEcho runs an echo target that only accepts TLS connections.
func startTLSEcho(t *testing.T, certPath, keyPath string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	creds, err := credentials.NewServerTLSFromFile(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(creds), grpc.UnknownServiceHandler(echoHandler))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

func TestProxy_ServesTLSToClients(t *testing.T) {
	ca := newTestCA(t)
	certPath, keyPath := ca.issue(t, "tamper")

	cfg := &config.Config{
		Listen: config.Listen{
			Addr: "127.0.0.1:0",
			TLS:  &config.ServerTLS{CertFile: certPath, KeyFile: keyPath},
		},
		Target: config.Target{Addr: startEcho(t)},
	}
	p := startProxyWithConfig(t, cfg, nil)

	pool, err := certPool(ca.certPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(p.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, "")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	req := rawBytes("hello")
	var resp rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp) != "hello" {
		t.Errorf("expected %q, got %q", "hello", resp)
	}
}

func TestProxy_DialsTargetOverTLS(t *testing.T) {
	ca := newTestCA(t)
	certPath, keyPath := ca.issue(t, "target")

	cfg := &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{
			Addr: startTLSEcho(t, certPath, keyPath),
			TLS:  &config.ClientTLS{CAFile: ca.certPath},
		},
	}
	p := startProxyWithConfig(t, cfg, nil)

	conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	req := rawBytes("hello")
	var resp rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp) != "hello" {
		t.Errorf("expected %q, got %q", "hello", resp)
	}
}

func TestNew_RejectsTargetSignedByUnknownCA(t *testing.T) {
	target := newTestCA(t)
	certPath, keyPath := target.issue(t, "target")
	stranger := newTestCA(t)

	cfg := &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{
			Addr: startTLSEcho(t, certPath, keyPath),
			TLS:  &config.ClientTLS{CAFile: stranger.certPath},
		},
	}

	_, err := New(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected New to reject a target signed by an unknown CA")
	}
	if !strings.Contains(err.Error(), "verify target certificate") {
		t.Errorf("expected a certificate error, got %v", err)
	}
}

func TestNew_ToleratesUnreachableTarget(t *testing.T) {
	ca := newTestCA(t)

	// Nothing listens on this address: the target is simply not up yet, which
	// must not stop the proxy from starting.
	cfg := &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{
			Addr: "127.0.0.1:1",
			TLS:  &config.ClientTLS{CAFile: ca.certPath},
		},
	}

	p, err := New(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("expected the proxy to start, got %v", err)
	}
	t.Cleanup(func() { p.lis.Close() })
}

func TestCertPool_RejectsFileWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-ca.pem")
	if err := os.WriteFile(path, []byte("just some text\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := certPool(path); err == nil {
		t.Fatal("expected an error for a file holding no certificates")
	}
}

func TestClientTLSConfig_NilWithoutTLSBlock(t *testing.T) {
	cfg, err := clientTLSConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected no TLS config, got %+v", cfg)
	}
}
