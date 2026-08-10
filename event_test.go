package filex

import (
	"context"
	"github.com/lcylpzls/testx"
	"strings"
	"sync"
	"testing"
)

func TestEventHook(t *testing.T) {
	hook := &fakeEventHook{}
	store := newEventStore(t, hook)
	defer store.Close()

	if _, err := store.Put(context.Background(), "bucket", "k", strings.NewReader("数据"), PutOptions{}); err != nil {
		t.Fatalf("Put 失败：%v", err)
	}
	obj, err := store.Get(context.Background(), "bucket", "k", GetOptions{})
	testx.RequireNoError(t, err)
	_ = obj.Close()
	testx.RequireNoError(t, store.Delete(context.Background(), "bucket", "k"))
	if _, err := store.Get(context.Background(), "bucket", "missing", GetOptions{}); err == nil {
		t.Fatal("Get 不存在的对象应失败")
	}

	events := hook.snapshot()
	if len(events) != 4 {
		t.Fatalf("期望 4 个事件，得到 %d：%+v", len(events), events)
	}
	if events[0].Action != "put" || events[0].Bucket != "bucket" || events[0].Key != "k" || events[0].Err != nil {
		t.Fatalf("Put 事件不匹配：%+v", events[0])
	}
	if events[1].Action != "get" || events[1].Err != nil {
		t.Fatalf("Get 事件不匹配：%+v", events[1])
	}
	if events[2].Action != "delete" {
		t.Fatalf("Delete 事件不匹配：%+v", events[2])
	}
	if events[3].Action != "get" || events[3].Err == nil {
		t.Fatalf("Get 失败事件应携带错误：%+v", events[3])
	}
}

func TestEventHookList(t *testing.T) {
	hook := &fakeEventHook{}
	store := newEventStore(t, hook)
	defer store.Close()
	_, _ = store.Put(context.Background(), "bucket", "k1", strings.NewReader("x"), PutOptions{})
	if _, err := store.List(context.Background(), "bucket", ListOptions{}); err != nil {
		t.Fatalf("List 失败：%v", err)
	}
	events := hook.snapshot()
	if len(events) != 2 {
		t.Fatalf("期望 2 个事件：%+v", events)
	}
	if events[1].Action != "list" || events[1].Key != "" {
		t.Fatalf("List 事件不匹配：%+v", events[1])
	}
}

func TestEventHookCopyMove(t *testing.T) {
	hook := &fakeEventHook{}
	store := newEventStore(t, hook)
	defer store.Close()
	_, _ = store.Put(context.Background(), "bucket", "src", strings.NewReader("x"), PutOptions{})
	if _, err := store.Copy(context.Background(), "bucket", "src", "bucket", "dst"); err != nil {
		t.Fatalf("Copy 失败：%v", err)
	}
	if _, err := store.Move(context.Background(), "bucket", "dst", "bucket", "moved"); err != nil {
		t.Fatalf("Move 失败：%v", err)
	}
	events := hook.snapshot()
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "put,copy,move" {
		t.Fatalf("操作事件不匹配：%v", actions)
	}
}

func TestNoEventHook(t *testing.T) {
	store := newEventStore(t, nil)
	defer store.Close()
	if _, err := store.Put(context.Background(), "bucket", "k", strings.NewReader("x"), PutOptions{}); err != nil {
		t.Fatalf("无钩子 Put 失败：%v", err)
	}
}

func newEventStore(t *testing.T, hook EventHook) *Store {
	t.Helper()
	store, err := New(Config{DataDir: t.TempDir(), EventHook: hook})
	testx.RequireNoError(t, err)
	if _, err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("CreateBucket 失败：%v", err)
	}
	return store
}

// fakeEventHook 记录事件。
type fakeEventHook struct {
	mu   sync.Mutex
	list []ObjectEvent
}

func (h *fakeEventHook) OnObjectEvent(_ context.Context, e ObjectEvent) {
	h.mu.Lock()
	h.list = append(h.list, e)
	h.mu.Unlock()
}

func (h *fakeEventHook) snapshot() []ObjectEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ObjectEvent(nil), h.list...)
}
