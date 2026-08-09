package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
)

type serverMetrics struct {
	adds map[string]int64
	errs map[string]int
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{adds: map[string]int64{}, errs: map[string]int{}}
}

func (m *serverMetrics) Add(bucket, op string, bytes int64) {
	m.adds[bucket+"/"+op] += bytes
}

func (m *serverMetrics) IncError(bucket, code string) {
	m.errs[bucket+"/"+code]++
}

func TestServerHealth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	c, err := client.New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("健康检查失败：%v", err)
	}

	f := &fakeStore{listBucketsErr: errors.New("存储不可用")}
	ts2 := httptest.NewServer(NewHandler(HandlerConfig{Store: f}))
	defer ts2.Close()
	resp, err := http.Get(ts2.URL + "/filex/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("健康检查失败状态码不符：%d", resp.StatusCode)
	}
}

func TestServerMetrics(t *testing.T) {
	store, _ := filex.New(filex.Config{DataDir: t.TempDir()})
	defer store.Close()
	metrics := newServerMetrics()
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, Metrics: metrics}))
	defer ts.Close()

	_, _ = http.Get(ts.URL + "/filex/v1/health")
	_, _ = http.Get(ts.URL + "/filex/v1/buckets/missing/objects/k")
	if metrics.adds["/http_request"] != 1 {
		t.Fatalf("成功请求指标不符：%+v", metrics.adds)
	}
	if metrics.errs["/404"] != 1 {
		t.Fatalf("错误请求指标不符：%+v", metrics.errs)
	}
}
