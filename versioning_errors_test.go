package filex

import (
	"context"
	"encoding/json"
	"errors"
	testx "github.com/lcylpzls/testx"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionOpsValidation(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	ops := []struct {
		name string
		run  func() error
	}{
		{"GetVersion 非法桶", func() error {
			_, err := s.GetVersion(ctx, "BAD", "k", "v", GetOptions{})
			return err
		}},
		{"GetVersion 非法键", func() error {
			_, err := s.GetVersion(ctx, "abc", "", "v", GetOptions{})
			return err
		}},
		{"GetVersion 缺失桶", func() error {
			_, err := s.GetVersion(ctx, "missing", "k", "v", GetOptions{})
			return err
		}},
		{"HeadVersion 非法桶", func() error {
			_, err := s.HeadVersion(ctx, "BAD", "k", "v")
			return err
		}},
		{"HeadVersion 非法键", func() error {
			_, err := s.HeadVersion(ctx, "abc", "", "v")
			return err
		}},
		{"HeadVersion 缺失桶", func() error {
			_, err := s.HeadVersion(ctx, "missing", "k", "v")
			return err
		}},
		{"DeleteVersion 非法桶", func() error {
			return s.DeleteVersion(ctx, "BAD", "k", "v")
		}},
		{"DeleteVersion 非法键", func() error {
			return s.DeleteVersion(ctx, "abc", "", "v")
		}},
		{"DeleteVersion 缺失桶", func() error {
			return s.DeleteVersion(ctx, "missing", "k", "v")
		}},
		{"ListVersions 非法桶", func() error {
			_, err := s.ListVersions(ctx, "BAD", "k")
			return err
		}},
		{"ListVersions 非法键", func() error {
			_, err := s.ListVersions(ctx, "abc", "")
			return err
		}},
		{"ListVersions 缺失桶", func() error {
			_, err := s.ListVersions(ctx, "missing", "k")
			return err
		}},
	}
	for _, op := range ops {
		if err := op.run(); err == nil {
			t.Errorf("%s 应报错", op.name)
		}
	}
}

func TestVersionMetaCorruption(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})

	metaPath := s.versionMetaPath("abc", "k", info.VersionID)
	_ = os.WriteFile(metaPath, []byte("{"), 0o644)
	if _, err := s.GetVersion(ctx, "abc", "k", info.VersionID, GetOptions{}); err == nil {
		t.Fatal("损坏版本元数据 GetVersion 应报错")
	}
	if _, err := s.HeadVersion(ctx, "abc", "k", info.VersionID); err == nil {
		t.Fatal("损坏版本元数据 HeadVersion 应报错")
	}
	if err := s.DeleteVersion(ctx, "abc", "k", info.VersionID); err == nil {
		t.Fatal("损坏版本元数据 DeleteVersion 应报错")
	}
}

func TestBucketVersioningConfigErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	// ensureBucket 成功后，bucketVersioning 再次读取桶元数据时注入失败
	injected := errors.New("注入错误")
	calls := 0
	s.fs.ReadFile = func(path string) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, injected
		}
		return os.ReadFile(path)
	}
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("Put 损坏桶元数据应报错")
	}
	calls = 0
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("Get 损坏桶元数据应报错")
	}
	calls = 0
	if _, err := s.Head(ctx, "abc", "k"); err == nil {
		t.Fatal("Head 损坏桶元数据应报错")
	}
	calls = 0
	if err := s.Delete(ctx, "abc", "k"); err == nil {
		t.Fatal("Delete 损坏桶元数据应报错")
	}
	calls = 0
	if _, err := s.List(ctx, "abc", ListOptions{}); err == nil {
		t.Fatal("List 损坏桶元数据应报错")
	}
}

func TestVersionReadDirErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})

	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); !errors.Is(err, injected) {
		t.Fatalf("Get ReadDir 失败应透传：%v", err)
	}
	if _, err := s.Head(ctx, "abc", "k"); !errors.Is(err, injected) {
		t.Fatalf("Head ReadDir 失败应透传：%v", err)
	}
	if _, err := s.ListVersions(ctx, "abc", "k"); !errors.Is(err, injected) {
		t.Fatalf("ListVersions ReadDir 失败应透传：%v", err)
	}
	if _, err := s.List(ctx, "abc", ListOptions{}); !errors.Is(err, injected) {
		t.Fatalf("List ReadDir 失败应透传：%v", err)
	}
}

func TestDeleteMarkerWriteError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})

	injected := errors.New("注入错误")
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		if strings.Contains(pattern, ".meta-") {
			return nil, injected
		}
		return os.CreateTemp(dir, pattern)
	}
	if err := s.Delete(ctx, "abc", "k"); !errors.Is(err, injected) {
		t.Fatalf("删除标记写入失败应透传：%v", err)
	}
}

func TestRemoveVersionFilesErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	injected := errors.New("注入错误")

	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".json") {
			return injected
		}
		return os.Remove(name)
	}
	if err := s.DeleteVersion(ctx, "abc", "k", info.VersionID); !errors.Is(err, injected) {
		t.Fatalf("元数据删除失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".data") {
			return injected
		}
		return os.Remove(name)
	}
	if err := s.DeleteVersion(ctx, "abc", "k", info.VersionID); !errors.Is(err, injected) {
		t.Fatalf("数据删除失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	// ReadDir 失败时删除仍成功（目录回收为尽力而为）
	info2, _ := s.Put(ctx, "abc", "k2", strings.NewReader("v"), PutOptions{})
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if err := s.DeleteVersion(ctx, "abc", "k2", info2.VersionID); err != nil {
		t.Fatalf("ReadDir 失败不应影响删除：%v", err)
	}
}

func TestMoveDeleteError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "src")
	mustBucket(t, s, "dst")
	ctx := context.Background()
	_, _ = s.Put(ctx, "src", "k", strings.NewReader("v"), PutOptions{})

	injected := errors.New("注入错误")
	s.fs.Remove = func(name string) error {
		if strings.Contains(name, "src") && strings.HasSuffix(name, ".json") {
			return injected
		}
		return os.Remove(name)
	}
	if _, err := s.Move(ctx, "src", "k", "dst", "k2"); !errors.Is(err, injected) {
		t.Fatalf("Move 源删除失败应透传：%v", err)
	}
}

func TestSetBucketQuotaErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	if _, err := s.SetBucketQuota(ctx, "BAD", 1); err == nil {
		t.Fatal("非法桶名应报错")
	}
	_ = os.WriteFile(s.bucketMetaPath("abc"), []byte("{"), 0o644)
	if _, err := s.SetBucketQuota(ctx, "abc", 1); err == nil {
		t.Fatal("损坏桶元数据应报错")
	}
	_, _ = s.CreateBucket(ctx, "abc2")
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.SetBucketQuota(ctx, "abc2", 1); !errors.Is(err, injected) {
		t.Fatalf("配额写入失败应透传：%v", err)
	}
}

func TestSetBucketVersioningCorrupt(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_ = os.WriteFile(s.bucketMetaPath("abc"), []byte("{"), 0o644)
	if _, err := s.SetBucketVersioning(ctx, "abc", true); err == nil {
		t.Fatal("损坏桶元数据应报错")
	}
}

func TestVersionEmptyIDBranches(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	if _, err := s.HeadVersion(ctx, "abc", "k", ""); err == nil {
		t.Fatal("HeadVersion 空版本 ID 应报错")
	}
	if err := s.DeleteVersion(ctx, "abc", "k", ""); err == nil {
		t.Fatal("DeleteVersion 空版本 ID 应报错")
	}
}

func TestVersionedHeadAfterDelete(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Delete(ctx, "abc", "k")
	if _, err := s.Head(ctx, "abc", "k"); err == nil {
		t.Fatal("软删除后 Head 应报错")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}
}

func TestVersionedDeleteReadError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if err := s.Delete(ctx, "abc", "k"); !errors.Is(err, injected) {
		t.Fatalf("Delete 版本读失败应透传：%v", err)
	}
}

func TestMoveCopyError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "src")
	mustBucket(t, s, "dst")
	if _, err := s.Move(context.Background(), "src", "missing", "dst", "x"); err == nil {
		t.Fatal("Move 缺失源应报错")
	}
}

func TestCollectMetasErrorBranches(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	injected := errors.New("注入错误")

	// 版本子目录读取失败：跳过
	subPath := s.versionDir("abc", "k")
	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == subPath {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	if _, err := s.List(ctx, "abc", ListOptions{}); err != nil {
		t.Fatalf("子目录读取失败应跳过：%v", err)
	}
	if _, err := s.bucketUsage("abc"); err != nil {
		t.Fatalf("bucketUsage 子目录读取失败应跳过：%v", err)
	}
	s.fs = defaultFSOps

	// 损坏版本元数据：跳过
	_ = os.WriteFile(s.versionMetaPath("abc", "k", info.VersionID), []byte("{"), 0o644)
	if _, err := s.List(ctx, "abc", ListOptions{}); err != nil {
		t.Fatalf("损坏版本元数据应跳过：%v", err)
	}
	if _, err := s.bucketUsage("abc"); err != nil {
		t.Fatalf("bucketUsage 损坏版本元数据应跳过：%v", err)
	}
}

func TestBucketUsageMissingObjectsDir(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	_ = os.RemoveAll(s.objectsDir("abc"))
	usage, err := s.bucketUsage("abc")
	if err != nil || usage != 0 {
		t.Fatalf("缺失对象目录应返回 0：%d, %v", usage, err)
	}
}

func TestBucketUsageErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	_, _ = s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{})
	// 损坏扁平元数据：跳过
	_ = os.WriteFile(s.objectMetaPath("abc", "k"), []byte("{"), 0o644)
	if _, err := s.bucketUsage("abc"); err != nil {
		t.Fatalf("损坏元数据应跳过：%v", err)
	}
	// ReadDir 失败：透传
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.bucketUsage("abc"); !errors.Is(err, injected) {
		t.Fatalf("ReadDir 失败应透传：%v", err)
	}
	// bucketVersioning 读取失败分支
	s.fs = defaultFSOps
	calls := 0
	s.fs.ReadFile = func(path string) ([]byte, error) {
		calls++
		if calls > 0 {
			return nil, injected
		}
		return os.ReadFile(path)
	}
	if _, err := s.bucketUsage("abc"); !errors.Is(err, injected) {
		t.Fatalf("bucketVersioning 读取失败应透传：%v", err)
	}
}

func TestCheckQuotaBucketUsageError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketQuota(ctx, "abc", 100)
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); err != nil {
		t.Fatalf("配额统计失败应忽略：%v", err)
	}
}

func TestRemoveVersionFilesEmptyDir(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	if err := s.DeleteVersion(ctx, "abc", "k", info.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.versionDir("abc", "k")); !os.IsNotExist(err) {
		t.Fatal("空版本目录应被回收")
	}
}

func TestCollectCurrentMetasCorruptFlat(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_ = os.WriteFile(s.objectMetaPath("abc", "k"), []byte("{"), 0o644)
	result, err := s.List(ctx, "abc", ListOptions{})
	testx.RequireNoError(t, err)

	if len(result.Objects) != 0 {
		t.Fatalf("损坏扁平元数据不应返回：%+v", result.Objects)
	}
}

func TestSortMetasTieBreak(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("a"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("b"), PutOptions{})

	// 手工写入两个同时间戳版本，验证 VersionID 降序兜底
	now := time.Now().UTC()
	v1 := newVersionID()
	v2 := newVersionID()
	if v2 < v1 {
		v1, v2 = v2, v1
	}
	dir := s.versionDir("abc", "k")
	_ = os.Remove(s.versionMetaPath("abc", "k", v1))
	_ = os.Remove(s.versionMetaPath("abc", "k", v2))
	for _, v := range []string{v1, v2} {
		meta := objectMeta{
			Key:       "k",
			Size:      1,
			SHA256:    strings.Repeat("a", 64),
			VersionID: v,
			CreatedAt: now,
			UpdatedAt: now,
		}
		data, _ := json.Marshal(meta)
		_ = os.WriteFile(filepath.Join(dir, "v-"+v+".json"), data, 0o644)
	}
	versions, err := s.ListVersions(ctx, "abc", "k")
	testx.RequireNoError(t, err)

	if len(versions) < 2 || versions[0].VersionID != v2 {
		t.Fatalf("同时间戳应按 VersionID 降序：%+v", versions)
	}
}

func TestCheckQuotaMissingBucket(t *testing.T) {
	s, _ := newStore(t)
	if err := s.checkQuota("missing"); err != nil {
		t.Fatalf("缺失桶配额检查应返回 nil：%v", err)
	}
}

func TestCollectMetasStrayFiles(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = os.WriteFile(filepath.Join(s.objectsDir("abc"), "junk"), []byte("x"), 0o644)

	result, err := s.List(ctx, "abc", ListOptions{})
	testx.RequireNoError(t, err)

	if len(result.Objects) != 1 {
		t.Fatalf("多余文件应跳过：%+v", result.Objects)
	}
	all, err := s.collectAllMetas("abc")
	if err != nil || len(all) != 1 {
		t.Fatalf("collectAllMetas 应跳过多余文件：%+v, %v", all, err)
	}
}
