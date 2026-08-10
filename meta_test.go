package filex

import (
	"encoding/json"
	"errors"
	testx "github.com/lcylpzls/testx"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObjectMetaValid(t *testing.T) {
	ok := objectMeta{Key: "k", Size: 1, SHA256: strings.Repeat("a", 64)}
	if err := ok.valid(); err != nil {
		t.Fatalf("合法元数据应通过：%v", err)
	}
	mustErrCode(t, (objectMeta{Size: 1, SHA256: strings.Repeat("a", 64)}).valid(), CodeMetadataCorrupt)
	mustErrCode(t, (objectMeta{Key: "k", Size: -1, SHA256: strings.Repeat("a", 64)}).valid(), CodeMetadataCorrupt)
	mustErrCode(t, (objectMeta{Key: "k", Size: 1, SHA256: "bad"}).valid(), CodeMetadataCorrupt)
}

func TestReadBucketMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")
	good := bucketMeta{Name: "abc", CreatedAt: time.Now().UTC()}
	data, _ := json.Marshal(good)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := readBucketMeta(defaultFSOps, path)
	testx.RequireNoError(t, err)

	testx.RequireEqual(t, meta.Name, "abc")

	if _, err := readBucketMeta(defaultFSOps, filepath.Join(dir, "missing.json")); !os.IsNotExist(err) {
		t.Fatalf("应返回不存在：%v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustErrCode(t, mustReadBucketMetaErr(t, bad), CodeMetadataCorrupt)
	noName := filepath.Join(dir, "noname.json")
	_ = os.WriteFile(noName, []byte(`{"created_at":"2026-01-01T00:00:00Z"}`), 0o644)
	mustErrCode(t, mustReadBucketMetaErr(t, noName), CodeMetadataCorrupt)
}

func TestReadObjectMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obj.json")
	good := objectMeta{Key: "k", Size: 2, SHA256: strings.Repeat("b", 64)}
	data, _ := json.Marshal(good)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := readObjectMeta(defaultFSOps, path)
	testx.RequireNoError(t, err)

	testx.RequireEqual(t, meta.Key, "k")

	if _, err := readObjectMeta(defaultFSOps, filepath.Join(dir, "missing.json")); !os.IsNotExist(err) {
		t.Fatalf("应返回不存在：%v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o644)
	mustErrCode(t, mustReadObjectMetaErr(t, bad), CodeMetadataCorrupt)
	invalid := filepath.Join(dir, "invalid.json")
	_ = os.WriteFile(invalid, []byte(`{"key":"k","size":1,"sha256":"zz"}`), 0o644)
	mustErrCode(t, mustReadObjectMetaErr(t, invalid), CodeMetadataCorrupt)
}

func mustReadBucketMetaErr(t *testing.T, path string) error {
	t.Helper()
	_, err := readBucketMeta(defaultFSOps, path)
	return err
}

func mustReadObjectMetaErr(t *testing.T, path string) error {
	t.Helper()
	_, err := readObjectMeta(defaultFSOps, path)
	return err
}

func TestWriteJSONAtomic(t *testing.T) {
	dir := t.TempDir()
	store := &Store{cfg: Config{}, fs: defaultFSOps}
	path := filepath.Join(dir, "nested", "meta.json")
	if err := store.writeJSONAtomic(path, bucketMeta{Name: "abc"}); err != nil {
		t.Fatalf("原子写入失败：%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("写入文件不存在：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nested", ".meta-")); err == nil {
		t.Fatal("临时文件不应残留")
	}

	injected := errors.New("注入错误")
	store.fs.MkdirAll = func(string, os.FileMode) error { return injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("MkdirAll 失败应透传：%v", err)
	}
	store.fs = defaultFSOps
	if err := store.writeJSONAtomic(path, make(chan int)); err == nil {
		t.Fatal("不可序列化值应报错")
	}
	store.fs = defaultFSOps
	store.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("CreateTemp 失败应透传：%v", err)
	}

	store.fs = defaultFSOps
	store.fs.WriteToFile = func(io.Writer, io.Reader) (int64, error) { return 0, injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("WriteToFile 失败应透传：%v", err)
	}

	store.fs = defaultFSOps
	store.fs.SyncFile = func(*os.File) error { return injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("SyncFile 失败应透传：%v", err)
	}

	store.cfg.DisableSync = true
	store.fs = defaultFSOps
	store.fs.CloseFile = func(f *os.File) error { _ = f.Close(); return injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("CloseFile 失败应透传：%v", err)
	}

	store.fs = defaultFSOps
	store.fs.Rename = func(string, string) error { return injected }
	if err := store.writeJSONAtomic(path, bucketMeta{}); !errors.Is(err, injected) {
		t.Fatalf("Rename 失败应透传：%v", err)
	}
}

func TestDefaultSyncPath(t *testing.T) {
	_ = defaultSyncPath(t.TempDir()) // Windows 不支持目录 sync，忽略结果
	if err := defaultSyncPath(filepath.Join(t.TempDir(), "不存在")); err == nil {
		t.Fatal("不存在的目录应返回错误")
	}
}
