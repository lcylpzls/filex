package filex

import (
	"testing"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/logx"
)

// fakeLogger 记录日志消息，用于断言日志分支。
type fakeLogger struct {
	infos []string
	warns []string
	errs  []string
}

func (f *fakeLogger) Info(msg string, _ logx.FieldGroup) { f.infos = append(f.infos, msg) }
func (f *fakeLogger) Warn(msg string, _ logx.FieldGroup) { f.warns = append(f.warns, msg) }
func (f *fakeLogger) Error(msg string, _ logx.FieldGroup) {
	f.errs = append(f.errs, msg)
}

// fakeMetrics 记录指标打点。
type fakeMetrics struct {
	adds map[string]int64
	errs map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{adds: map[string]int64{}, errs: map[string]int{}}
}

func (f *fakeMetrics) Add(bucket, op string, bytes int64) {
	f.adds[bucket+"/"+op] += bytes
}

func (f *fakeMetrics) IncError(bucket, code string) {
	f.errs[bucket+"/"+code]++
}

// mustErrCode 断言错误包含指定 errx 错误码。
func mustErrCode(t *testing.T, err error, code errx.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %s，实际无错误", code)
	}
	if !errx.Is(err, code) {
		t.Fatalf("期望错误码 %s，实际 %v", code, err)
	}
}
