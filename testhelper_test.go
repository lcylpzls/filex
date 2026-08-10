package filex

import (
	testx "github.com/lcylpzls/testx"
	"sync"
	"testing"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/logx"
)

// fakeLogger 记录日志消息，用于断言日志分支。
type fakeLogger struct {
	mu    sync.Mutex
	infos []string
	warns []string
	errs  []string
}

func (f *fakeLogger) Info(msg string, _ logx.FieldGroup) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infos = append(f.infos, msg)
}
func (f *fakeLogger) Warn(msg string, _ logx.FieldGroup) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warns = append(f.warns, msg)
}
func (f *fakeLogger) Error(msg string, _ logx.FieldGroup) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, msg)
}

// fakeMetrics 记录指标打点。
type fakeMetrics struct {
	mu   sync.Mutex
	adds map[string]int64
	errs map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{adds: map[string]int64{}, errs: map[string]int{}}
}

func (f *fakeMetrics) IncCounter(name string, labels []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "filex.errors" || len(labels) < 4 {
		return
	}
	f.errs[labels[1]+"/"+labels[3]]++
}

func (f *fakeMetrics) AddCounter(name string, delta float64, labels []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "filex.bytes" || len(labels) < 4 {
		return
	}
	f.adds[labels[1]+"/"+labels[3]] += int64(delta)
}

func (f *fakeMetrics) ObserveDuration(string, float64, []string) {}

func (f *fakeMetrics) AddGauge(string, float64, []string) {}

func (f *fakeMetrics) SetGauge(string, float64, []string) {}

func (f *fakeMetrics) RegisterMetric(string, string, []string) error {
	return nil
}

// mustErrCode 断言错误包含指定 errx 错误码。
func mustErrCode(t *testing.T, err error, code errx.Code) {
	t.Helper()
	testx.RequireError(t, err)

	if !errx.Is(err, code) {
		t.Fatalf("期望错误码 %s，实际 %v", code, err)
	}
}
