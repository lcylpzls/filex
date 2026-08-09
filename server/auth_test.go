package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
)

func authDo(t *testing.T, ts *httptest.Server, method, path string, hdr http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Get("X-Filex-Timestamp") == "" {
		hdr = hdr.Clone()
		hdr.Set("X-Filex-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestServerAuthToken(t *testing.T) {
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, Token: "secret"}))
	defer ts.Close()

	resp := authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺少令牌状态码不符：%d", resp.StatusCode)
	}
	resp = authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{"Authorization": {"Bearer wrong"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误令牌状态码不符：%d", resp.StatusCode)
	}
	resp = authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{"Authorization": {"Bearer secret"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("正确令牌状态码不符：%d", resp.StatusCode)
	}
}

func TestServerAuthTimestamp(t *testing.T) {
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, Token: "secret"}))
	defer ts.Close()

	// 缺少时间戳
	req, _ := http.NewRequest("PUT", ts.URL+"/filex/v1/buckets/abc", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺少时间戳状态码不符：%d", resp.StatusCode)
	}
	// 非法时间戳
	resp = authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{"Authorization": {"Bearer secret"}, "X-Filex-Timestamp": {"abc"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("非法时间戳状态码不符：%d", resp.StatusCode)
	}
	// 过期时间戳
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	resp = authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{"Authorization": {"Bearer secret"}, "X-Filex-Timestamp": {stale}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("过期时间戳状态码不符：%d", resp.StatusCode)
	}
}

func TestServerAuthHMAC(t *testing.T) {
	secret := []byte("hmac-secret")
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, HMACSecret: secret}))
	defer ts.Close()

	c, err := client.New(ts.URL, client.WithHMAC(secret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBucket(context.Background(), "abc"); err != nil {
		t.Fatalf("HMAC 客户端失败：%v", err)
	}

	resp := authDo(t, ts, "GET", "/filex/v1/buckets", http.Header{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺少签名状态码不符：%d", resp.StatusCode)
	}
}

func TestServerAuthCallbackAndAudit(t *testing.T) {
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	var audits []AuditEvent
	ts := httptest.NewServer(NewHandler(HandlerConfig{
		Store: store,
		Authenticate: func(r *http.Request) error {
			if r.Header.Get("X-User") != "ok" {
				return errors.New("拒绝")
			}
			return nil
		},
		Audit: func(e AuditEvent) { audits = append(audits, e) },
	}))
	defer ts.Close()

	resp := authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("回调拒绝状态码不符：%d", resp.StatusCode)
	}
	resp = authDo(t, ts, "PUT", "/filex/v1/buckets/abc", http.Header{"X-User": {"ok"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("回调通过状态码不符：%d", resp.StatusCode)
	}
	if len(audits) != 2 {
		t.Fatalf("审计事件数量不符：%d", len(audits))
	}
	if audits[1].Subject != "callback" || audits[1].Status != http.StatusCreated {
		t.Fatalf("审计事件内容不符：%+v", audits[1])
	}
}

func TestServerAuthErrorCode(t *testing.T) {
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, Token: "s"}))
	defer ts.Close()
	resp := authDo(t, ts, "GET", "/filex/v1/buckets", http.Header{})
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), string(errx.Code("filex_unauthorized"))) {
		t.Fatalf("错误体缺少未认证码：%s", body[:n])
	}
}
