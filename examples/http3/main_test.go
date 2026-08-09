package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lcylpzls/filex"
	filexclient "github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/httpx"
	_ "github.com/lcylpzls/httpx/http3" // 注册 HTTP/3 传输
	"github.com/lcylpzls/logx"
)

func writeTestCert(t *testing.T) (string, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}), 0o600)
	return certFile, keyFile
}

func testLogger() logx.Logger {
	logger, err := logx.NewBuilder().EnableWriter(io.Discard, logx.InfoLevel).Build()
	if err != nil {
		panic(err)
	}
	return logger
}

// h3Transport 把 httpx.Client 适配为标准库 http.RoundTripper。
type h3Transport struct {
	c *httpx.Client
}

func (t *h3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.c.Do(req.Context(), req)
}

func TestHTTP3RoundTrip(t *testing.T) {
	certFile, keyFile := writeTestCert(t)
	store, err := filex.New(filex.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var mu sync.Mutex
	var protos []string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, addr, err := startAndWait(ctx, serverConfig{
		Store: store,
		OnRequest: func(p string) {
			mu.Lock()
			protos = append(protos, p)
			mu.Unlock()
		},
	}, certFile, keyFile, "127.0.0.1:0", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	h3, err := httpx.New(
		httpx.WithTimeout(10*time.Second),
		httpx.WithProtocol(httpx.ProtocolHTTP3),
		httpx.WithTLSClientConfig(&tls.Config{InsecureSkipVerify: true}),
	)
	if err != nil {
		t.Fatal(err)
	}
	c, err := filexclient.New("https://"+addr,
		filexclient.WithHTTPClient(&http.Client{Transport: &h3Transport{c: h3}}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.CreateBucket(ctx, "demo")
	_, err = c.Put(ctx, "demo", "hello.txt", strings.NewReader("你好，HTTP/3"),
		filex.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("HTTP/3 写入失败：%v", err)
	}
	obj, err := c.Get(ctx, "demo", "hello.txt", filex.GetOptions{Verify: true})
	if err != nil {
		t.Fatalf("HTTP/3 读取失败：%v", err)
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != "你好，HTTP/3" {
		t.Fatalf("HTTP/3 内容不符：%q, %v", data, err)
	}

	mu.Lock()
	defer mu.Unlock()
	hit := false
	for _, p := range protos {
		if p == "HTTP/3.0" {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("未观察到 HTTP/3 请求，实际协议：%v", protos)
	}
}
