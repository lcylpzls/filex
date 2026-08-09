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

func TestSetBucketLifecycleCorruptMeta(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	_ = os.WriteFile(s.bucketMetaPath("abc"), []byte("{"), 0o644)
	if _, err := s.SetBucketLifecycle(context.Background(), "abc", LifecycleOptions{ExpireDays: 1}); err == nil {
		t.Fatal("损坏桶元数据应报错")
	}
}

func TestRunLifecycleMoreErrors(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.RunLifecycle(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}
	mustBucket(t, s, "abc")
	// 未配置生命周期 → 空报告
	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil || report.Scanned != 0 {
		t.Fatalf("未配置生命周期应返回空报告：%+v, %v", report, err)
	}
	// 损坏桶元数据
	_ = os.WriteFile(s.bucketMetaPath("abc"), []byte("{"), 0o644)
	if _, err := s.RunLifecycle(ctx, "abc"); err == nil {
		t.Fatal("损坏桶元数据应报错")
	}
}

func TestRunLifecycleVersioningError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1})
	injected := errors.New("注入错误")
	calls := 0
	s.fs.ReadFile = func(path string) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, injected
		}
		return os.ReadFile(path)
	}
	if _, err := s.RunLifecycle(ctx, "abc"); !errors.Is(err, injected) {
		t.Fatalf("版本配置读取失败应透传：%v", err)
	}
}

func TestRunLifecycleExpireVersionedAndPruneError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1, MaxVersions: 1})
	old, _ := s.Put(ctx, "abc", "k", strings.NewReader("old"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("new"), PutOptions{})

	// 老化旧版本后：过期清理删除旧版本
	meta, _ := readObjectMeta(s.fs, s.versionMetaPath("abc", "k", old.VersionID))
	meta.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(s.versionMetaPath("abc", "k", old.VersionID), data, 0o644)
	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if report.Expired != 1 {
		t.Fatalf("版本化过期清理不符：%+v", report)
	}

	// 版本收敛删除失败 → 记录消息
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v2"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v3"), PutOptions{})
	injected := errors.New("注入错误")
	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".json") && strings.Contains(name, "v-") {
			return injected
		}
		return os.Remove(name)
	}
	report, err = s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Messages) == 0 || report.Pruned != 0 {
		t.Fatalf("版本收敛失败应记录消息：%+v", report)
	}
}

func TestRemoveObjectFilesDataError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	meta, _ := readObjectMeta(s.fs, s.objectMetaPath("abc", "k"))
	meta.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(s.objectMetaPath("abc", "k"), data, 0o644)

	injected := errors.New("注入错误")
	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".data") {
			return injected
		}
		return os.Remove(name)
	}
	report, err := s.RunLifecycle(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Messages) == 0 {
		t.Fatalf("数据删除失败应记录消息：%+v", report)
	}
}

func TestSweepOrphansMore(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.SweepOrphans(ctx); !errors.Is(err, injected) {
		t.Fatalf("桶目录读取失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	_ = os.WriteFile(filepath.Join(s.bucketsDir, "not-dir.txt"), []byte("x"), 0o644)
	_ = os.RemoveAll(s.objectsDir("abc"))
	report, err := s.SweepOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Buckets != 1 || report.RemovedData != 0 {
		t.Fatalf("缺失对象目录应跳过：%+v", report)
	}

	// 对象目录读取失败
	_ = os.MkdirAll(s.objectsDir("abc"), 0o755)
	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == s.objectsDir("abc") {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	report, err = s.SweepOrphans(ctx)
	if err != nil || report.Buckets != 1 {
		t.Fatalf("对象目录读取失败应跳过：%+v, %v", report, err)
	}
	s.fs = defaultFSOps

	// 版本目录：有效数据+元数据保留；孤儿数据删除；临时文件删除
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "vk", strings.NewReader("v"), PutOptions{})
	vdir := s.versionDir("abc", "vk")
	// 版本目录读取失败：跳过
	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == vdir {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	report, err = s.SweepOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.fs = defaultFSOps
	// 临时文件清理，有效版本保留
	_ = os.WriteFile(filepath.Join(vdir, ".tmp-x"), []byte("t"), 0o644)
	report, err = s.SweepOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.RemovedTmp != 1 {
		t.Fatalf("版本目录临时文件清理不符：%+v", report)
	}
	if _, err := os.Stat(s.versionMetaPath("abc", "vk", info.VersionID)); err != nil {
		t.Fatal("有效版本不应被删除")
	}

	// 活动分片会话目录不应被孤儿巡检删除
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "upload-key", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "upload-key", up.UploadID, 1, strings.NewReader("p"))
	report, err = s.SweepOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.uploadMetaPath("abc", up.UploadID)); err != nil {
		t.Fatal("活动分片会话不应被清理")
	}
}
