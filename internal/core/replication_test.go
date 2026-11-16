package core

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lcylpzls/errx"
	testx "github.com/lcylpzls/testx"
)

func TestReplicaVersionRoundTrip(t *testing.T) {
	store, _ := newStore(t)
	mustBucket(t, store, "abc")
	ctx := context.Background()
	_, _ = store.SetBucketVersioning(ctx, "abc", true)
	created := time.Date(2026, 11, 16, 9, 30, 0, 123, time.UTC)
	updated := created.Add(time.Minute)
	content := "replica-data"
	opts := ReplicaOptions{
		VersionID:   "source-version-1",
		SHA256:      sha256Hex(content),
		ContentType: "text/plain",
		Metadata:    map[string]string{"source": "center-a"},
		CreatedAt:   created,
		UpdatedAt:   updated,
	}
	info, err := store.PutReplica(ctx, "abc", "dir/object.txt", strings.NewReader(content), opts)
	testx.RequireNoError(t, err)
	if info.VersionID != opts.VersionID || !info.CreatedAt.Equal(created) || !info.UpdatedAt.Equal(updated) {
		t.Fatalf("副本元数据不符：%+v", info)
	}

	same, err := store.PutReplica(ctx, "abc", "dir/object.txt", strings.NewReader(content), opts)
	testx.RequireNoError(t, err)
	if same.ETag != info.ETag {
		t.Fatalf("幂等写入结果不符：%+v", same)
	}
	object, err := store.GetVersion(ctx, "abc", "dir/object.txt", opts.VersionID, GetOptions{Verify: true})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != content {
		t.Fatalf("副本内容不符：%s", data)
	}

	opts.SHA256 = sha256Hex("different")
	if _, err := store.PutReplica(ctx, "abc", "dir/object.txt", strings.NewReader("different"), opts); err == nil {
		t.Fatal("同版本冲突应报错")
	} else {
		mustErrCode(t, err, CodePreconditionFailed)
	}
}

func TestReplicaDeleteMarkersAndListAllVersions(t *testing.T) {
	store, _ := newStore(t)
	mustBucket(t, store, "abc")
	ctx := context.Background()
	_, _ = store.SetBucketVersioning(ctx, "abc", true)
	created := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	_, err := store.PutReplica(ctx, "abc", "key", strings.NewReader("value"), ReplicaOptions{
		VersionID: "source-object",
		SHA256:    sha256Hex("value"),
		CreatedAt: created,
		UpdatedAt: created,
	})
	testx.RequireNoError(t, err)
	_, err = store.PutReplica(ctx, "abc", "key", nil, ReplicaOptions{
		VersionID: "source-delete",
		Deleted:   true,
		CreatedAt: created.Add(time.Second),
		UpdatedAt: created.Add(time.Second),
	})
	testx.RequireNoError(t, err)

	versions, err := store.ListAllVersions(ctx, "abc")
	testx.RequireNoError(t, err)
	if len(versions) != 2 || !versions[0].Deleted || versions[1].Deleted {
		t.Fatalf("全量版本列表不符：%+v", versions)
	}
	if _, err := store.Get(ctx, "abc", "key", GetOptions{}); err == nil {
		t.Fatal("删除标记应隐藏当前对象")
	}
}

func TestReplicaUnversionedAndValidation(t *testing.T) {
	store, _ := newStore(t)
	mustBucket(t, store, "abc")
	ctx := context.Background()
	content := "flat"
	opts := ReplicaOptions{
		SHA256:    sha256Hex(content),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	info, err := store.PutReplica(ctx, "abc", "flat", strings.NewReader(content), opts)
	testx.RequireNoError(t, err)
	if info.VersionID != "" {
		t.Fatalf("非版本化副本不应有版本 ID：%+v", info)
	}
	bucket, err := store.GetBucket(ctx, "abc")
	testx.RequireNoError(t, err)
	if bucket.Name != "abc" {
		t.Fatalf("GetBucket 结果不符：%+v", bucket)
	}

	versioned := opts
	versioned.VersionID = "version-1"
	if _, err := store.PutReplica(ctx, "abc", "bad", strings.NewReader(content), versioned); err == nil {
		t.Fatal("非版本化桶中的版本副本应拒绝")
	}
	_, _ = store.SetBucketVersioning(ctx, "abc", true)
	if _, err := store.PutReplica(ctx, "abc", "bad", nil, opts); err == nil {
		t.Fatal("缺少读取器应报错")
	}
	opts.VersionID = "bad-version"
	opts.Deleted = true
	opts.CreatedAt = time.Time{}
	if _, err := store.PutReplica(ctx, "abc", "bad", nil, opts); err != nil {
		t.Fatalf("零值时间应自动补齐：%v", err)
	}
	opts.VersionID = ""
	if _, err := store.PutReplica(ctx, "abc", "bad", nil, opts); err == nil {
		t.Fatal("删除标记缺少版本 ID 应报错")
	}
	opts.VersionID = "../escape"
	opts.Deleted = false
	if _, err := store.PutReplica(ctx, "abc", "bad", strings.NewReader(content), opts); err == nil {
		t.Fatal("非法版本 ID 应报错")
	}
	opts.VersionID = "valid"
	opts.CreatedAt = time.Now().UTC()
	opts.UpdatedAt = opts.CreatedAt.Add(-time.Second)
	if _, err := store.PutReplica(ctx, "abc", "bad", strings.NewReader(content), opts); err == nil {
		t.Fatal("倒序时间应报错")
	}
	opts.UpdatedAt = opts.CreatedAt
	opts.SHA256 = "bad"
	if _, err := store.PutReplica(ctx, "abc", "bad", strings.NewReader(content), opts); err == nil {
		t.Fatal("非法 SHA256 应报错")
	}

	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := store.GetVersion(ctx, "abc", "k", "../escape", GetOptions{})
			return err
		},
		"head": func() error {
			_, err := store.HeadVersion(ctx, "abc", "k", "../escape")
			return err
		},
		"delete": func() error {
			return store.DeleteVersion(ctx, "abc", "k", "../escape")
		},
	} {
		t.Run(name, func(t *testing.T) {
			mustErrCode(t, call(), CodeInvalidArgument)
		})
	}
}

func TestReplicaMetadataBranchesAndWriteErrors(t *testing.T) {
	store, _ := newStore(t)
	mustBucket(t, store, "abc")
	ctx := context.Background()
	_, _ = store.SetBucketVersioning(ctx, "abc", true)
	now := time.Now().UTC()
	opts := ReplicaOptions{
		VersionID: "source-a",
		SHA256:    sha256Hex("value"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := store.PutReplica(ctx, "abc", "key", strings.NewReader("value"), opts)
	testx.RequireNoError(t, err)
	opts.Metadata = map[string]string{"different": "metadata"}
	if _, err := store.PutReplica(ctx, "abc", "key", strings.NewReader("value"), opts); err == nil {
		t.Fatal("元数据冲突应报错")
	}

	marker := ReplicaOptions{
		VersionID: "source-delete",
		Deleted:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = store.PutReplica(ctx, "abc", "key", nil, marker)
	testx.RequireNoError(t, err)
	_, err = store.PutReplica(ctx, "abc", "key", nil, marker)
	testx.RequireNoError(t, err)

	corruptID := "corrupt"
	corruptPath := store.versionMetaPath("abc", "key", corruptID)
	testx.RequireNoError(t, os.MkdirAll(filepath.Dir(corruptPath), 0o755))
	testx.RequireNoError(t, os.WriteFile(corruptPath, []byte("{"), 0o644))
	_, err = store.PutReplica(ctx, "abc", "key", strings.NewReader("value"), ReplicaOptions{
		VersionID: corruptID,
		SHA256:    sha256Hex("value"),
		CreatedAt: now,
		UpdatedAt: now,
	})
	mustErrCode(t, err, CodeStorageFailed)

	injected := errors.New("注入错误")
	originalCreateTemp := store.fs.CreateTemp
	store.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	_, err = store.PutReplica(ctx, "abc", "marker", nil, ReplicaOptions{
		VersionID: "marker-error",
		Deleted:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	mustErrCode(t, err, CodeStorageFailed)
	store.fs.CreateTemp = originalCreateTemp
}

func TestListAllVersionsBranches(t *testing.T) {
	store, _ := newStore(t)
	mustBucket(t, store, "abc")
	ctx := context.Background()
	_, _ = store.Put(ctx, "abc", "a", strings.NewReader("a"), PutOptions{})
	_, _ = store.Put(ctx, "abc", "b", strings.NewReader("legacy-b"), PutOptions{})
	_, _ = store.SetBucketVersioning(ctx, "abc", true)
	now := time.Now().UTC()
	for _, versionID := range []string{"version-a", "version-b"} {
		_, err := store.PutReplica(ctx, "abc", "b", strings.NewReader(versionID), ReplicaOptions{
			VersionID: versionID,
			SHA256:    sha256Hex(versionID),
			CreatedAt: now,
			UpdatedAt: now,
		})
		testx.RequireNoError(t, err)
	}
	objectsDir := store.objectsDir("abc")
	testx.RequireNoError(t, os.Mkdir(filepath.Join(objectsDir, ".uploads"), 0o755))
	testx.RequireNoError(t, os.Mkdir(filepath.Join(objectsDir, "broken"), 0o755))
	testx.RequireNoError(t, os.WriteFile(filepath.Join(objectsDir, "notes.txt"), []byte("x"), 0o644))
	testx.RequireNoError(t, os.WriteFile(filepath.Join(objectsDir, "corrupt.json"), []byte("{"), 0o644))
	corruptVersionDir := filepath.Join(objectsDir, hashKey("corrupt-version"))
	testx.RequireNoError(t, os.Mkdir(corruptVersionDir, 0o755))
	testx.RequireNoError(t, os.WriteFile(filepath.Join(corruptVersionDir, "v-bad.json"), []byte("{"), 0o644))

	originalReadDir := store.fs.ReadDir
	store.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if filepath.Base(path) == "broken" {
			return nil, injectedError()
		}
		return originalReadDir(path)
	}
	versions, err := store.ListAllVersions(ctx, "abc")
	testx.RequireNoError(t, err)
	if len(versions) != 4 || versions[len(versions)-1].Key != "b" {
		t.Fatalf("全量版本分支结果不符：%+v", versions)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListAllVersions(cancelled, "abc"); !errx.Is(err, CodeCancelled) {
		t.Fatalf("取消错误码不符：%v", err)
	}
	if _, err := store.ListAllVersions(ctx, "BAD"); !errx.Is(err, CodeInvalidBucket) {
		t.Fatalf("非法桶错误码不符：%v", err)
	}
	if _, err := store.ListAllVersions(ctx, "missing"); !errx.Is(err, CodeBucketNotFound) {
		t.Fatalf("缺失桶错误码不符：%v", err)
	}
	if _, err := store.GetVersion(ctx, "abc", "k", strings.Repeat("x", 129), GetOptions{}); !errx.Is(err, CodeInvalidArgument) {
		t.Fatalf("超长版本 ID 错误码不符：%v", err)
	}

	store.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == objectsDir {
			return nil, injectedError()
		}
		return originalReadDir(path)
	}
	if _, err := store.ListAllVersions(ctx, "abc"); !errx.Is(err, CodeStorageFailed) {
		t.Fatalf("扫描错误码不符：%v", err)
	}
	store.fs.ReadDir = originalReadDir
	_ = store.Close()
	if _, err := store.ListAllVersions(ctx, "abc"); !errx.Is(err, CodeClosed) {
		t.Fatalf("关闭态错误码不符：%v", err)
	}
}

func injectedError() error {
	return errors.New("注入错误")
}
