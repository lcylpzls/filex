package core

import (
	"bytes"
	"context"
	testx "github.com/lcylpzls/testx"
	"strings"
	"sync"
	"testing"
)

// traceCall 记录一次追踪调用。
type traceCall struct {
	name  string
	attrs map[string]string
	err   error
	ended bool
}

// fakeTraceHook 内存追踪钩子。
type fakeTraceHook struct {
	mu    sync.Mutex
	calls []traceCall
}

func (h *fakeTraceHook) Start(ctx context.Context, name string, attrs ...TraceAttr) (context.Context, func(error)) {
	h.mu.Lock()
	h.calls = append(h.calls, traceCall{name: name, attrs: map[string]string{}})
	for _, a := range attrs {
		h.calls[len(h.calls)-1].attrs[a.Key] = a.Value
	}
	h.mu.Unlock()
	return ctx, func(err error) {
		h.mu.Lock()
		h.calls[len(h.calls)-1].err = err
		h.calls[len(h.calls)-1].ended = true
		h.mu.Unlock()
	}
}

func (h *fakeTraceHook) snapshot() []traceCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]traceCall, len(h.calls))
	copy(out, h.calls)
	return out
}

// TestTraceHook 覆盖对象操作追踪埋点（成功与失败）。
func TestTraceHook(t *testing.T) {
	hook := &fakeTraceHook{}
	s, err := New(Config{DataDir: t.TempDir(), TraceHook: hook})
	testx.RequireNoError(t, err)

	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, "bucket", "key1", bytes.NewReader([]byte("data")), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "bucket", "missing", GetOptions{}); err == nil {
		t.Fatal("缺失对象应报错")
	}

	calls := hook.snapshot()
	if len(calls) != 2 {
		t.Fatalf("应调用 2 次追踪钩子，实际：%d", len(calls))
	}
	for i, c := range calls {
		if !strings.HasPrefix(c.name, "filex.") || c.attrs["filex.operation"] == "" ||
			c.attrs["filex.bucket"] != "bucket" || !c.ended {
			t.Fatalf("第 %d 次追踪调用不符：%+v", i, c)
		}
	}
	if calls[0].name != "filex.put" || calls[0].attrs["filex.key"] != "key1" || calls[0].err != nil {
		t.Fatalf("put 埋点不符：%+v", calls[0])
	}
	if calls[1].name != "filex.get" || calls[1].err == nil {
		t.Fatalf("get 失败埋点不符：%+v", calls[1])
	}
}
