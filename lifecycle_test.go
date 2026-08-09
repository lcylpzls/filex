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

type countingCtx struct {
	ctx   context.Context
	n     int
	limit int
}

func (c *countingCtx) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c *countingCtx) Done() <-chan struct{}       { return c.ctx.Done() }
func (c *countingCtx) Err() error {
	c.n++
	if c.n > c.limit {
		return context.Canceled
	}
	return nil
}
func (c *countingCtx) Value(key any) any { return c.ctx.Value(key) }

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

func ageUploadSession(t *testing.T, s *Store, bucket, uploadID string, hours int) {
	t.Helper()
	meta, err := readUploadMeta(s.fs, s.uploadMetaPath(bucket, uploadID))
	if err != nil {
		t.Fatal(err)
	}
	meta.CreatedAt = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(s.uploadMetaPath(bucket, uploadID), data, 0o644)
}

func TestSweepStaleUploads(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	fresh, _ := s.InitiateMultipartUpload(ctx, "abc", "fresh", PutOptions{})
	stale, _ := s.InitiateMultipartUpload(ctx, "abc", "stale", PutOptions{})
	ageUploadSession(t, s, "abc", stale.UploadID, 48)

	report, err := s.SweepOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.RemovedSessions != 1 {
		t.Fatalf("过期会话清理数量不符：%+v", report)
	}
	if _, err := os.Stat(s.uploadMetaPath("abc", fresh.UploadID)); err != nil {
		t.Fatal("新鲜会话不应被清理")
	}
	if _, err := os.Stat(s.uploadMetaPath("abc", stale.UploadID)); !os.IsNotExist(err) {
		t.Fatal("过期会话应被清理")
	}
}

func TestSweepStaleUploadsErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	if n := s.sweepStaleUploads(filepath.Join(s.objectsDir("abc"), ".uploads")); n != 0 {
		t.Fatalf("缺失上传目录应返回 0：%d", n)
	}
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	ageUploadSession(t, s, "abc", up.UploadID, 48)
	injected := errors.New("注入错误")

	uploadsDir := filepath.Join(s.objectsDir("abc"), ".uploads")
	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == uploadsDir {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	report, err := s.SweepOrphans(ctx)
	if err != nil || report.RemovedSessions != 0 {
		t.Fatalf("读取失败应跳过：%+v, %v", report, err)
	}
	s.fs = defaultFSOps

	// 会话目录内非目录项与损坏元数据：跳过
	_ = os.WriteFile(filepath.Join(uploadsDir, "junk"), []byte("x"), 0o644)
	bad := filepath.Join(uploadsDir, "bad")
	_ = os.MkdirAll(bad, 0o755)
	_ = os.WriteFile(filepath.Join(bad, "upload.json"), []byte("{"), 0o644)
	report, err = s.SweepOrphans(ctx)
	if err != nil || report.RemovedSessions != 1 {
		t.Fatalf("损坏会话应跳过且过期会话清理：%+v, %v", report, err)
	}
}

func TestDeleteBucketActiveUploads(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	if err := s.DeleteBucket(ctx, "abc"); err == nil {
		t.Fatal("活动分片上传应阻止删除桶")
	} else {
		mustErrCode(t, err, CodeBucketNotEmpty)
	}
	_ = s.AbortMultipartUpload(ctx, "abc", "k", up.UploadID)
	if err := s.DeleteBucket(ctx, "abc"); err != nil {
		t.Fatalf("中止后删除桶失败：%v", err)
	}

	// 过期会话不阻止删除桶
	mustBucket(t, s, "abc2")
	up2, _ := s.InitiateMultipartUpload(ctx, "abc2", "k", PutOptions{})
	ageUploadSession(t, s, "abc2", up2.UploadID, 48)
	if err := s.DeleteBucket(ctx, "abc2"); err != nil {
		t.Fatalf("过期会话不应阻止删除桶：%v", err)
	}

	// .uploads 目录读取失败与非目录项：不阻止删除
	mustBucket(t, s, "abc3")
	uploadsDir := filepath.Join(s.objectsDir("abc3"), ".uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir, "junk"), []byte("x"), 0o644)
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == uploadsDir {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	if err := s.DeleteBucket(ctx, "abc3"); err != nil {
		t.Fatalf("上传目录读取失败不应阻止删除：%v", err)
	}
	s.fs = defaultFSOps

	// .uploads 中只有非目录项：不阻止删除
	mustBucket(t, s, "abc5")
	uploadsDir5 := filepath.Join(s.objectsDir("abc5"), ".uploads")
	_ = os.MkdirAll(uploadsDir5, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir5, "junk"), []byte("x"), 0o644)
	if err := s.DeleteBucket(ctx, "abc5"); err != nil {
		t.Fatalf("非目录项不应阻止删除：%v", err)
	}

	// 无 .uploads 目录：孤儿巡检正常
	mustBucket(t, s, "abc4")
	report, err := s.SweepOrphans(ctx)
	if err != nil || report.RemovedSessions != 0 {
		t.Fatalf("无上传目录巡检不符：%+v, %v", report, err)
	}
}

func TestLifecycleContextCancel(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{ExpireDays: 1})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.RunLifecycle(cancelCtx, "abc"); err == nil {
		t.Fatal("取消上下文 RunLifecycle 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}
	if _, err := s.SweepOrphans(cancelCtx); err == nil {
		t.Fatal("取消上下文 SweepOrphans 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}
}

func TestLifecycleContextCancelInnerLoops(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{MaxVersions: 1})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v1"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v2"), PutOptions{})

	// 过期循环放行 2 次，版本收敛循环处取消
	runCtx := &countingCtx{ctx: ctx, limit: 2}
	if _, err := s.RunLifecycle(runCtx, "abc"); err == nil {
		t.Fatal("版本收敛循环取消应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}

	// 桶循环放行 1 次，对象条目循环处取消
	sweepCtx := &countingCtx{ctx: ctx, limit: 1}
	if _, err := s.SweepOrphans(sweepCtx); err == nil {
		t.Fatal("对象条目循环取消应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}
}
