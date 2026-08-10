package filex

import (
	"context"
	"errors"
	testx "github.com/lcylpzls/testx"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{
		DataDir: dir,
		Logger:  &fakeLogger{},
		Metrics: newFakeMetrics(),
	})
	testx.RequireNoError(t, err)

	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func TestNew(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("空数据目录应报错")
	} else {
		mustErrCode(t, err, CodeInvalidConfig)
	}
	if _, err := New(Config{DataDir: t.TempDir(), MaxObjectSize: -1}); err == nil {
		t.Fatal("负数大小上限应报错")
	}
	if _, err := New(Config{DataDir: t.TempDir(), MaxKeyBytes: -1}); err == nil {
		t.Fatal("负数键上限应报错")
	}
	if _, err := New(Config{DataDir: "a\x00b"}); err == nil {
		t.Log("NUL 路径未报错，跳过 Abs 分支")
	}

	blocker := filepath.Join(t.TempDir(), "文件")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{DataDir: filepath.Join(blocker, "子目录")}); err == nil {
		t.Fatal("目录创建失败应报错")
	} else {
		mustErrCode(t, err, CodeStorageFailed)
	}

	s, dir := newStore(t)
	if _, err := os.Stat(filepath.Join(dir, "buckets")); err != nil {
		t.Fatalf("buckets 目录未创建：%v", err)
	}
	testx.RequireNoError(t, s.Close())
}

func TestCreateBucket(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	info, err := s.CreateBucket(ctx, "my-bucket")
	testx.RequireNoError(t, err)

	testx.RequireEqual(t, info.Name, "my-bucket")

	if _, err := s.CreateBucket(ctx, "my-bucket"); err == nil {
		t.Fatal("重复创建应报错")
	} else {
		mustErrCode(t, err, CodeBucketExists)
	}
	if _, err := s.CreateBucket(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}

	injected := errors.New("注入错误")
	s.fs.ReadFile = func(string) ([]byte, error) { return nil, injected }
	if _, err := s.CreateBucket(ctx, "my-bucket"); !errors.Is(err, injected) {
		t.Fatalf("检查桶读失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.MkdirAll = func(string, os.FileMode) error { return injected }
	if _, err := s.CreateBucket(ctx, "new-bucket"); !errors.Is(err, injected) {
		t.Fatalf("MkdirAll 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.CreateBucket(ctx, "new-bucket"); !errors.Is(err, injected) {
		t.Fatalf("CreateTemp 失败应透传：%v", err)
	}
}

func TestDeleteBucket(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.DeleteBucket(ctx, "missing"); err == nil {
		t.Fatal("删除不存在的桶应报错")
	} else {
		mustErrCode(t, err, CodeBucketNotFound)
	}
	if err := s.DeleteBucket(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}

	_, _ = s.CreateBucket(ctx, "abc")
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	if err := s.DeleteBucket(ctx, "abc"); err == nil {
		t.Fatal("非空桶应拒绝删除")
	} else {
		mustErrCode(t, err, CodeBucketNotEmpty)
	}
	_ = s.Delete(ctx, "abc", "k")
	testx.RequireNoError(t, s.DeleteBucket(ctx, "abc"))

	_, _ = s.CreateBucket(ctx, "orphan")
	_, _ = s.Put(ctx, "orphan", "k", strings.NewReader("v"), PutOptions{})
	// 制造孤儿数据：只删元数据文件
	_ = os.Remove(s.objectMetaPath("orphan", "k"))
	testx.RequireNoError(t, s.DeleteBucket(ctx, "orphan"))

	injected := errors.New("注入错误")
	_, _ = s.CreateBucket(ctx, "scanfail")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if err := s.DeleteBucket(ctx, "scanfail"); err == nil {
		t.Fatal("ReadDir 失败应报错")
	}
	s.fs = defaultFSOps

	_, _ = s.CreateBucket(ctx, "corrupt")
	metaPath := s.bucketMetaPath("corrupt")
	_ = os.WriteFile(metaPath, []byte("{"), 0o644)
	if err := s.DeleteBucket(ctx, "corrupt"); err == nil {
		t.Fatal("损坏桶元数据应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	_, _ = s.CreateBucket(ctx, "rmfail")
	s.fs.RemoveAll = func(string) error { return injected }
	if err := s.DeleteBucket(ctx, "rmfail"); !errors.Is(err, injected) {
		t.Fatalf("RemoveAll 失败应透传：%v", err)
	}
}

func TestHeadBucket(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.HeadBucket(ctx, "missing"); err == nil {
		t.Fatal("不存在的桶应报错")
	} else {
		mustErrCode(t, err, CodeBucketNotFound)
	}
	_, _ = s.CreateBucket(ctx, "abc")
	info, err := s.HeadBucket(ctx, "abc")
	testx.RequireNoError(t, err)

	testx.RequireEqual(t, info.Name, "abc")

	if _, err := s.HeadBucket(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}
}

func TestListBuckets(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	_, _ = s.CreateBucket(ctx, "bbb")
	_, _ = s.CreateBucket(ctx, "aaa")
	buckets, err := s.ListBuckets(ctx)
	testx.RequireNoError(t, err)

	if len(buckets) != 2 || buckets[0].Name != "aaa" || buckets[1].Name != "bbb" {
		t.Fatalf("桶列表应排序：%+v", buckets)
	}

	badDir := filepath.Join(s.bucketsDir, "bad")
	_ = os.MkdirAll(badDir, 0o755)
	_ = os.WriteFile(filepath.Join(badDir, "meta.json"), []byte("{"), 0o644)
	_ = os.WriteFile(filepath.Join(s.bucketsDir, "not-dir.txt"), []byte("x"), 0o644)
	buckets, err = s.ListBuckets(ctx)
	testx.RequireNoError(t, err)

	if len(buckets) != 2 {
		t.Fatalf("损坏桶应被跳过：%+v", buckets)
	}

	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("注入错误") }
	if _, err := s.ListBuckets(ctx); err == nil {
		t.Fatal("ReadDir 失败应报错")
	}
}
