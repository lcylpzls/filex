package filex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetBucketLifecycle(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	info, err := s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 7, MaxVersions: 3})
	if err != nil || info.Lifecycle.ExpireDays != 7 || info.Lifecycle.MaxVersions != 3 {
		t.Fatalf("设置生命周期失败：%+v, %v", info, err)
	}
	info, err = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{})
	if err != nil || info.Lifecycle != (LifecycleOptions{}) {
		t.Fatalf("清空生命周期失败：%+v, %v", info, err)
	}
	if _, err := s.SetBucketLifecycle(ctx, "missing", LifecycleOptions{ExpireDays: 1}); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.SetBucketLifecycle(ctx, "BAD", LifecycleOptions{}); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: -1}); err == nil {
		t.Fatal("负数过期天数应报错")
	}
	if _, err := s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{MaxVersions: -1}); err == nil {
		t.Fatal("负数版本上限应报错")
	}

	injected := errors.New("注入错误")
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1}); !errors.Is(err, injected) {
		t.Fatalf("写入失败应透传：%v", err)
	}
}

func ageObject(t *testing.T, s *Store, bucket, key string, days int) {
	t.Helper()
	meta, err := readObjectMeta(s.fs, s.objectMetaPath(bucket, key))
	if err != nil {
		t.Fatal(err)
	}
	meta.UpdatedAt = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(s.objectMetaPath(bucket, key), data, 0o644)
}

func TestRunLifecycleExpire(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "old", strings.NewReader("old"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "new", strings.NewReader("new"), PutOptions{})
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1})
	ageObject(t, s, "abc", "old", 2)

	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatalf("RunLifecycle 失败：%v", err)
	}
	if report.Scanned != 2 || report.Expired != 1 {
		t.Fatalf("清理报告不符：%+v", report)
	}
	if _, err := s.Head(ctx, "abc", "old"); err == nil {
		t.Fatal("过期对象应被删除")
	}
	if _, err := s.Head(ctx, "abc", "new"); err != nil {
		t.Fatalf("未过期对象应保留：%v", err)
	}
}

func TestRunLifecycleMaxVersions(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{MaxVersions: 2})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v1"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v2"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v3"), PutOptions{})

	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if report.Pruned != 1 {
		t.Fatalf("版本收敛数量不符：%+v", report)
	}
	versions, _ := s.ListVersions(ctx, "abc", "k")
	if len(versions) != 2 {
		t.Fatalf("应保留 2 个版本：%+v", versions)
	}
}

func TestRunLifecycleErrors(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.RunLifecycle(ctx, "missing"); err == nil {
		t.Fatal("缺失桶应报错")
	}
	mustBucket(t, s, "abc")
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	ageObject(t, s, "abc", "k", 2)

	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.RunLifecycle(ctx, "abc"); !errors.Is(err, injected) {
		t.Fatalf("ReadDir 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".json") {
			return injected
		}
		return os.Remove(name)
	}
	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Messages) == 0 || report.Expired != 0 {
		t.Fatalf("删除失败应记录消息：%+v", report)
	}
}

func TestSweepOrphans(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = os.Remove(s.objectMetaPath("abc", "k"))
	tmp := filepath.Join(s.objectsDir("abc"), ".tmp-orphan")
	_ = os.WriteFile(tmp, []byte("x"), 0o644)

	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "vk", strings.NewReader("v"), PutOptions{})
	_ = os.Remove(s.versionMetaPath("abc", "vk", info.VersionID))

	report, err := s.SweepOrphans(ctx)
	if err != nil {
		t.Fatalf("SweepOrphans 失败：%v", err)
	}
	if report.Buckets != 1 || report.RemovedData != 2 || report.RemovedTmp != 1 {
		t.Fatalf("孤儿巡检报告不符：%+v", report)
	}
	if _, err := os.Stat(s.versionDir("abc", "vk")); !os.IsNotExist(err) {
		t.Fatal("空版本目录应被回收")
	}
}

func TestHealth(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Health(context.Background()); err != nil {
		t.Fatalf("健康检查失败：%v", err)
	}
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if err := s.Health(context.Background()); !errors.Is(err, injected) {
		t.Fatalf("健康检查失败应透传：%v", err)
	}
}
