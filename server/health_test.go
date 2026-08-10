package server

import (
	"context"
	"errors"
	testx "github.com/lcylpzls/testx"
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
	testx.RequireNoError(t, err)

	testx.RequireNoError(t, c.Health(context.Background()))

	f := &fakeStore{listBucketsErr: errors.New("存储不可用")}
	ts2 := httptest.NewServer(NewHandler(HandlerConfig{Store: f}))
	defer ts2.Close()
	resp, err := http.Get(ts2.URL + "/filex/v1/health")
	testx.RequireNoError(t, err)

	_ = resp.Body.Close()
	testx.RequireEqual(t, resp.StatusCode, http.StatusInternalServerError)

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
