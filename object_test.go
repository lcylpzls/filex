package filex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	testx "github.com/lcylpzls/testx"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustBucket(t *testing.T, s *Store, name string) {
	t.Helper()
	if _, err := s.CreateBucket(context.Background(), name); err != nil {
		t.Fatalf("创建桶失败：%v", err)
	}
}

func mustPut(t *testing.T, s *Store, bucket, key, content string, opts PutOptions) ObjectInfo {
	t.Helper()
	info, err := s.Put(context.Background(), bucket, key, strings.NewReader(content), opts)
	testx.RequireNoError(t, err)

	return info
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestPutSuccess(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")

	info := mustPut(t, s, "abc", "a.txt", "hello", PutOptions{
		ContentType: "text/plain",
		Metadata:    map[string]string{"owner": "me"},
	})
	if info.ETag != sha256Hex("hello") {
		t.Fatalf("ETag 应为内容 SHA256：%s", info.ETag)
	}
	if info.ContentType != "text/plain" || info.Metadata["owner"] != "me" {
		t.Fatalf("元数据不符：%+v", info)
	}
	if info.Size != 5 {
		t.Fatalf("大小不符：%d", info.Size)
	}

	// 覆盖写入
	info2 := mustPut(t, s, "abc", "a.txt", "world", PutOptions{})
	testx.RequireNotEqual(t, info2.ETag, info.ETag)

	testx.RequireEqual(t, info2.ContentType, "application/octet-stream")

	// 期望校验通过
	info3 := mustPut(t, s, "abc", "b.txt", "data", PutOptions{
		ExpectedSHA256: sha256Hex("data"),
	})
	if info3.ETag != sha256Hex("data") {
		t.Fatalf("期望校验对象 ETag 不符：%s", info3.ETag)
	}
}

func TestPutErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	if _, err := s.Put(ctx, "abc", "k", nil, PutOptions{}); err == nil {
		t.Fatal("nil 读取器应报错")
	} else {
		mustErrCode(t, err, CodeInvalidArgument)
	}
	if _, err := s.Put(ctx, "BAD", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.Put(ctx, "abc", "", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("非法键应报错")
	}
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{
		Metadata: map[string]string{"": "v"},
	}); err == nil {
		t.Fatal("非法元数据应报错")
	}
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{
		ExpectedSHA256: "bad",
	}); err == nil {
		t.Fatal("非法期望哈希应报错")
	}
	if _, err := s.Put(ctx, "missing", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("不存在的桶应报错")
	} else {
		mustErrCode(t, err, CodeBucketNotFound)
	}

	// 期望校验失败
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{
		ExpectedSHA256: strings.Repeat("0", 64),
	}); err == nil {
		t.Fatal("期望校验失败应报错")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}

	// 键哈希冲突
	keyB := "b-key"
	collisionPath := s.objectMetaPath("abc", keyB)
	_ = os.MkdirAll(filepath.Dir(collisionPath), 0o755)
	fakeMeta, _ := json.Marshal(objectMeta{Key: "a-key", Size: 1, SHA256: strings.Repeat("a", 64)})
	_ = os.WriteFile(collisionPath, fakeMeta, 0o644)
	if _, err := s.Put(ctx, "abc", keyB, strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("键哈希冲突应报错")
	} else {
		mustErrCode(t, err, CodeStorageFailed)
	}
}

func TestPutIOErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	s.fs.MkdirAll = func(string, os.FileMode) error { return injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("MkdirAll 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("CreateTemp 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.WriteToFile = func(io.Writer, io.Reader) (int64, error) { return 0, injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("WriteToFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.SyncFile = func(*os.File) error { return injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("SyncFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CloseFile = func(f *os.File) error { _ = f.Close(); return injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("CloseFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.Rename = func(string, string) error { return injected }
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("Rename 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		if strings.Contains(pattern, ".meta-") {
			return nil, injected
		}
		return os.CreateTemp(dir, pattern)
	}
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("元数据 CreateTemp 失败应透传：%v", err)
	}
}

func TestPutTooLarge(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, MaxObjectSize: 4})
	testx.RequireNoError(t, err)

	defer s.Close()
	mustBucket(t, s, "abc")
	if _, err := s.Put(context.Background(), "abc", "k", strings.NewReader("hello"), PutOptions{}); err == nil {
		t.Fatal("超限对象应报错")
	} else {
		mustErrCode(t, err, CodeObjectTooLarge)
	}
}

func TestGetSuccess(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	mustPut(t, s, "abc", "a.txt", "hello", PutOptions{ContentType: "text/plain"})

	obj, err := s.Get(ctx, "abc", "a.txt", GetOptions{})
	testx.RequireNoError(t, err)

	data, err := io.ReadAll(obj)
	testx.RequireNoError(t, err)

	if string(data) != "hello" {
		t.Fatalf("内容不符：%s", data)
	}
	_ = obj.Close()

	obj2, err := s.Get(ctx, "abc", "a.txt", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	data2, err := io.ReadAll(obj2)
	testx.RequireNoError(t, err)

	if string(data2) != "hello" {
		t.Fatalf("校验内容不符：%s", data2)
	}
	_ = obj2.Close()
}

func TestGetVerifyMismatch(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	mustPut(t, s, "abc", "a.txt", "hello", PutOptions{})

	dataPath := s.objectDataPath("abc", "a.txt")
	f, err := os.OpenFile(dataPath, os.O_WRONLY, 0)
	testx.RequireNoError(t, err)

	_, _ = f.WriteAt([]byte("X"), 0)
	_ = f.Close()

	obj, err := s.Get(ctx, "abc", "a.txt", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	defer obj.Close()
	if _, err := io.ReadAll(obj); err == nil {
		t.Fatal("校验失败应报错")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
}

func TestGetRange(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	mustPut(t, s, "abc", "a.txt", "hello", PutOptions{})

	readRange := func(rng ByteRange) string {
		t.Helper()
		obj, err := s.Get(ctx, "abc", "a.txt", GetOptions{Range: &rng})
		testx.RequireNoError(t, err)

		defer obj.Close()
		data, err := io.ReadAll(obj)
		testx.RequireNoError(t, err)

		return string(data)
	}

	if got := readRange(ByteRange{Start: 1, End: 3}); got != "ell" {
		t.Fatalf("范围 1-3 应为 ell，实际 %q", got)
	}
	if got := readRange(ByteRange{Start: 3, End: 100}); got != "lo" {
		t.Fatalf("范围越界应截断为 lo，实际 %q", got)
	}
	if got := readRange(ByteRange{Start: 0, End: 0}); got != "h" {
		t.Fatalf("单字节范围应为 h，实际 %q", got)
	}

	bad := []ByteRange{{Start: -1, End: 2}, {Start: 3, End: 1}, {Start: 5, End: 9}}
	for _, rng := range bad {
		if _, err := s.Get(ctx, "abc", "a.txt", GetOptions{Range: &rng}); err == nil {
			t.Fatalf("非法范围应报错：%+v", rng)
		} else {
			mustErrCode(t, err, CodeInvalidRange)
		}
	}

	if _, err := s.Get(ctx, "abc", "a.txt", GetOptions{Verify: true, Range: &ByteRange{Start: 0, End: 1}}); err == nil {
		t.Fatal("范围与校验互斥应报错")
	} else {
		mustErrCode(t, err, CodeInvalidArgument)
	}
}

func TestGetErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	if _, err := s.Get(ctx, "BAD", "k", GetOptions{}); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.Get(ctx, "abc", "", GetOptions{}); err == nil {
		t.Fatal("非法键应报错")
	}
	if _, err := s.Get(ctx, "missing", "k", GetOptions{}); err == nil {
		t.Fatal("不存在的桶应报错")
	} else {
		mustErrCode(t, err, CodeBucketNotFound)
	}
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("不存在的对象应报错")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}

	mustPut(t, s, "abc", "k", "v", PutOptions{})
	metaPath := s.objectMetaPath("abc", "k")
	_ = os.WriteFile(metaPath, []byte("{"), 0o644)
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("损坏元数据应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	// 恢复元数据，删除数据文件
	mustPut(t, s, "abc", "k", "v", PutOptions{})
	_ = os.Remove(s.objectDataPath("abc", "k"))
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("数据缺失应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	// 恢复对象并改大小
	mustPut(t, s, "abc", "k", "vvvv", PutOptions{})
	_ = os.Truncate(s.objectDataPath("abc", "k"), 2)
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("大小不一致应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	// Stat 注入失败
	mustPut(t, s, "abc", "k2", "v", PutOptions{})
	injected := errors.New("注入错误")
	s.fs.Stat = func(string) (os.FileInfo, error) { return nil, injected }
	if _, err := s.Get(ctx, "abc", "k2", GetOptions{}); !errors.Is(err, injected) {
		t.Fatalf("Stat 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.OpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, injected }
	if _, err := s.Get(ctx, "abc", "k2", GetOptions{}); !errors.Is(err, injected) {
		t.Fatalf("OpenFile 失败应透传：%v", err)
	}
}

func TestHead(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	mustPut(t, s, "abc", "k", "v", PutOptions{ContentType: "text/plain"})

	info, err := s.Head(ctx, "abc", "k")
	testx.RequireNoError(t, err)

	if info.ETag != sha256Hex("v") || info.ContentType != "text/plain" {
		t.Fatalf("Head 元数据不符：%+v", info)
	}
	if _, err := s.Head(ctx, "abc", "missing"); err == nil {
		t.Fatal("不存在的对象应报错")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}
	if _, err := s.Head(ctx, "missing", "k"); err == nil {
		t.Fatal("不存在的桶应报错")
	}
	if _, err := s.Head(ctx, "BAD", "k"); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.Head(ctx, "abc", ""); err == nil {
		t.Fatal("非法键应报错")
	}

	metaPath := s.objectMetaPath("abc", "k")
	_ = os.WriteFile(metaPath, []byte("{"), 0o644)
	if _, err := s.Head(ctx, "abc", "k"); err == nil {
		t.Fatal("损坏元数据应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	// 无 ContentType 的旧元数据应回退默认值
	nakedKey := "naked"
	nakedMeta, _ := json.Marshal(objectMeta{
		Key:    nakedKey,
		Size:   1,
		SHA256: strings.Repeat("a", 64),
	})
	_ = os.WriteFile(s.objectMetaPath("abc", nakedKey), nakedMeta, 0o644)
	nakedInfo, err := s.Head(ctx, "abc", nakedKey)
	testx.RequireNoError(t, err)

	testx.RequireEqual(t, nakedInfo.ContentType, "application/octet-stream")

}

func TestDelete(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	mustPut(t, s, "abc", "k", "v", PutOptions{})

	testx.RequireNoError(t, s.Delete(ctx, "abc", "k"))
	if err := s.Delete(ctx, "abc", "k"); err == nil {
		t.Fatal("重复删除应报错")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}
	if err := s.Delete(ctx, "abc", "missing"); err == nil {
		t.Fatal("不存在的对象应报错")
	}
	if err := s.Delete(ctx, "missing", "k"); err == nil {
		t.Fatal("不存在的桶应报错")
	}
	if err := s.Delete(ctx, "BAD", "k"); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if err := s.Delete(ctx, "abc", ""); err == nil {
		t.Fatal("非法键应报错")
	}

	mustPut(t, s, "abc", "corrupt", "v", PutOptions{})
	metaPath := s.objectMetaPath("abc", "corrupt")
	_ = os.WriteFile(metaPath, []byte("{"), 0o644)
	if err := s.Delete(ctx, "abc", "corrupt"); err == nil {
		t.Fatal("损坏元数据应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	// 数据文件已缺失时删除成功
	mustPut(t, s, "abc", "orphan", "v", PutOptions{})
	_ = os.Remove(s.objectDataPath("abc", "orphan"))
	testx.RequireNoError(t, s.Delete(ctx, "abc", "orphan"))

	// 数据删除失败仅告警，返回成功
	mustPut(t, s, "abc", "warn", "v", PutOptions{})
	injected := errors.New("注入错误")
	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".data") {
			return injected
		}
		return os.Remove(name)
	}
	testx.RequireNoError(t, s.Delete(ctx, "abc", "warn"))
	s.fs = defaultFSOps

	// 元数据删除失败
	mustPut(t, s, "abc", "metafail", "v", PutOptions{})
	s.fs.Remove = func(name string) error {
		if strings.HasSuffix(name, ".json") {
			return injected
		}
		return os.Remove(name)
	}
	if err := s.Delete(ctx, "abc", "metafail"); !errors.Is(err, injected) {
		t.Fatalf("元数据删除失败应透传：%v", err)
	}
}

func TestList(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	keys := []string{"a.txt", "dir/b.txt", "dir/c.txt", "x/y.txt"}
	for _, k := range keys {
		mustPut(t, s, "abc", k, "v", PutOptions{})
	}

	result, err := s.List(ctx, "abc", ListOptions{Limit: 2, Delimiter: "/"})
	testx.RequireNoError(t, err)

	if len(result.Objects) != 1 || result.Objects[0].Key != "a.txt" {
		t.Fatalf("对象列表不符：%+v", result.Objects)
	}
	if len(result.CommonPrefixes) != 1 || result.CommonPrefixes[0] != "dir/" {
		t.Fatalf("公共前缀不符：%v", result.CommonPrefixes)
	}
	if !result.IsTruncated || result.NextMarker != "dir/b.txt" {
		t.Fatalf("分页状态不符：%+v", result)
	}

	next, err := s.List(ctx, "abc", ListOptions{Marker: result.NextMarker, Delimiter: "/"})
	testx.RequireNoError(t, err)

	if len(next.Objects) != 0 || len(next.CommonPrefixes) != 2 ||
		next.CommonPrefixes[0] != "dir/" || next.CommonPrefixes[1] != "x/" {
		t.Fatalf("第二页不符：%+v", next)
	}

	prefixed, err := s.List(ctx, "abc", ListOptions{Prefix: "dir/", Delimiter: "/"})
	testx.RequireNoError(t, err)

	if len(prefixed.Objects) != 2 || len(prefixed.CommonPrefixes) != 0 {
		t.Fatalf("前缀过滤不符：%+v", prefixed)
	}

	one, err := s.List(ctx, "abc", ListOptions{Limit: 1})
	testx.RequireNoError(t, err)

	if len(one.Objects) != 1 || !one.IsTruncated || one.Objects[0].Key != "a.txt" {
		t.Fatalf("对象截断不符：%+v", one)
	}

	if _, err := s.List(ctx, "abc", ListOptions{Limit: -1}); err == nil {
		t.Fatal("负数 limit 应报错")
	}
	if _, err := s.List(ctx, "abc", ListOptions{Limit: maxListLimit + 1}); err == nil {
		t.Fatal("超限 limit 应报错")
	}
	if _, err := s.List(ctx, "missing", ListOptions{}); err == nil {
		t.Fatal("不存在的桶应报错")
	}
	if _, err := s.List(ctx, "BAD", ListOptions{}); err == nil {
		t.Fatal("非法桶名应报错")
	}

	mustBucket(t, s, "empty")
	empty, err := s.List(ctx, "empty", ListOptions{})
	if err != nil || len(empty.Objects) != 0 {
		t.Fatalf("空桶列表应为空：%+v, %v", empty, err)
	}
	// objects 目录缺失时应返回空结果
	_ = os.RemoveAll(s.objectsDir("empty"))
	missingDir, err := s.List(ctx, "empty", ListOptions{})
	if err != nil || len(missingDir.Objects) != 0 {
		t.Fatalf("缺失对象目录应返回空：%+v, %v", missingDir, err)
	}

	// 同一公共前缀多次出现只聚合一次
	all, err := s.List(ctx, "abc", ListOptions{Limit: 10, Delimiter: "/"})
	testx.RequireNoError(t, err)

	if len(all.CommonPrefixes) != 2 || all.CommonPrefixes[0] != "dir/" || all.CommonPrefixes[1] != "x/" {
		t.Fatalf("公共前缀应去重：%+v", all.CommonPrefixes)
	}

	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.List(ctx, "abc", ListOptions{}); !errors.Is(err, injected) {
		t.Fatalf("ReadDir 失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	// 损坏元数据跳过
	badKey := "bad-meta"
	badMetaPath := s.objectMetaPath("abc", badKey)
	_ = os.MkdirAll(filepath.Dir(badMetaPath), 0o755)
	_ = os.WriteFile(badMetaPath, []byte("{"), 0o644)
	result2, err := s.List(ctx, "abc", ListOptions{})
	testx.RequireNoError(t, err)

	for _, o := range result2.Objects {
		testx.RequireNotEqual(t, o.Key, badKey)

	}
}

func TestListContextCancel(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.List(ctx, "abc", ListOptions{}); err == nil {
		t.Fatal("取消上下文 List 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}
}

func TestNopMetrics(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir})
	testx.RequireNoError(t, err)

	defer s.Close()
	mustBucket(t, s, "abc")
	mustPut(t, s, "abc", "k", "v", PutOptions{})
	obj, err := s.Get(context.Background(), "abc", "k", GetOptions{})
	testx.RequireNoError(t, err)

	_ = obj.Close()
	testx.RequireNoError(t, s.Delete(context.Background(), "abc", "k"))
	if _, err := s.Put(context.Background(), "missing", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("不存在的桶应报错（触发 IncError 分支）")
	}
}

func TestEnsureBucketCorruptAndReadError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	_ = os.WriteFile(s.bucketMetaPath("abc"), []byte("{"), 0o644)
	if _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("损坏桶元数据应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}

	s2, _ := newStore(t)
	mustBucket(t, s2, "abc")
	injected := errors.New("注入错误")
	s2.fs.ReadFile = func(string) ([]byte, error) { return nil, injected }
	if _, err := s2.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("桶元数据读失败应透传：%v", err)
	}
}

func TestVerifyingReaderDirect(t *testing.T) {
	v := newVerifyingReader(strings.NewReader("abc"), strings.Repeat("0", 64))
	if _, err := io.ReadAll(v); err == nil {
		t.Fatal("校验失败应报错")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
	_ = v.Close()

	ok := newVerifyingReader(bytes.NewReader([]byte("abc")), sha256Hex("abc"))
	if _, err := io.ReadAll(ok); err != nil {
		t.Fatalf("校验成功不应报错：%v", err)
	}
}
