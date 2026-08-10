package filex

import (
	"context"
	"errors"
	testx "github.com/lcylpzls/testx"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBucketMetadata(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	info, err := s.CreateBucket(ctx, "abc")
	testx.RequireNoError(t, err)

	if info.Versioning || info.Quota != 0 {
		t.Fatalf("默认桶元数据不符：%+v", info)
	}

	info, err = s.SetBucketVersioning(ctx, "abc", true)
	if err != nil || !info.Versioning {
		t.Fatalf("开启版本化失败：%+v, %v", info, err)
	}
	info, err = s.SetBucketQuota(ctx, "abc", 100)
	if err != nil || info.Quota != 100 {
		t.Fatalf("设置配额失败：%+v, %v", info, err)
	}
	head, err := s.HeadBucket(ctx, "abc")
	if err != nil || !head.Versioning || head.Quota != 100 {
		t.Fatalf("HeadBucket 元数据不符：%+v, %v", head, err)
	}
	buckets, _ := s.ListBuckets(ctx)
	if len(buckets) != 1 || !buckets[0].Versioning {
		t.Fatalf("ListBuckets 元数据不符：%+v", buckets)
	}

	if _, err := s.SetBucketVersioning(ctx, "missing", true); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.SetBucketVersioning(ctx, "BAD", true); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.SetBucketQuota(ctx, "missing", 1); err == nil {
		t.Fatal("缺失桶配额应报错")
	}
	if _, err := s.SetBucketQuota(ctx, "abc", -1); err == nil {
		t.Fatal("负数配额应报错")
	}

	injected := errors.New("注入错误")
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.SetBucketVersioning(ctx, "abc", false); !errors.Is(err, injected) {
		t.Fatalf("版本化写入失败应透传：%v", err)
	}
}

func TestVersionedLifecycle(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)

	v1, _ := s.Put(ctx, "abc", "k", strings.NewReader("aaa"), PutOptions{ContentType: "text/plain"})
	v2, _ := s.Put(ctx, "abc", "k", strings.NewReader("bbb"), PutOptions{})
	if v1.VersionID == "" || v1.VersionID == v2.VersionID {
		t.Fatalf("版本 ID 不符：%+v %+v", v1, v2)
	}
	head, err := s.Head(ctx, "abc", "k")
	if err != nil || head.VersionID != v2.VersionID || head.ETag != sha256Hex("bbb") {
		t.Fatalf("Head 应指向最新版本：%+v, %v", head, err)
	}
	obj, _ := s.Get(ctx, "abc", "k", GetOptions{Verify: true})
	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	if string(data) != "bbb" {
		t.Fatalf("当前内容不符：%s", data)
	}

	versions, err := s.ListVersions(ctx, "abc", "k")
	if err != nil || len(versions) != 2 || versions[0].VersionID != v2.VersionID {
		t.Fatalf("版本列表不符：%+v, %v", versions, err)
	}
	old, err := s.GetVersion(ctx, "abc", "k", v1.VersionID, GetOptions{})
	testx.RequireNoError(t, err)

	oldData, _ := io.ReadAll(old)
	_ = old.Close()
	if string(oldData) != "aaa" {
		t.Fatalf("历史版本内容不符：%s", oldData)
	}
	oldHead, err := s.HeadVersion(ctx, "abc", "k", v1.VersionID)
	if err != nil || oldHead.ETag != sha256Hex("aaa") {
		t.Fatalf("HeadVersion 不符：%+v, %v", oldHead, err)
	}

	// 软删除
	if err := s.Delete(ctx, "abc", "k"); err != nil {
		t.Fatalf("软删除失败：%v", err)
	}
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("软删除后对象不可见")
	}
	versions, _ = s.ListVersions(ctx, "abc", "k")
	if len(versions) != 3 || !versions[0].Deleted {
		t.Fatalf("删除标记不符：%+v", versions)
	}
	list, _ := s.List(ctx, "abc", ListOptions{})
	if len(list.Objects) != 0 {
		t.Fatalf("软删除后列表应为空：%+v", list.Objects)
	}

	// 恢复历史版本
	restored, err := s.RestoreVersion(ctx, "abc", "k", v1.VersionID)
	testx.RequireNoError(t, err)

	if restored.VersionID == v1.VersionID || restored.ETag != sha256Hex("aaa") {
		t.Fatalf("恢复版本不符：%+v", restored)
	}
	obj2, _ := s.Get(ctx, "abc", "k", GetOptions{})
	data2, _ := io.ReadAll(obj2)
	_ = obj2.Close()
	if string(data2) != "aaa" {
		t.Fatalf("恢复后内容不符：%s", data2)
	}

	// 永久删除指定版本
	if err := s.DeleteVersion(ctx, "abc", "k", versions[0].VersionID); err != nil {
		t.Fatalf("删除标记失败：%v", err)
	}
	if err := s.DeleteVersion(ctx, "abc", "k", v1.VersionID); err != nil {
		t.Fatalf("删除历史版本失败：%v", err)
	}
	if _, err := s.GetVersion(ctx, "abc", "k", v1.VersionID, GetOptions{}); err == nil {
		t.Fatal("已删版本应报错")
	} else {
		mustErrCode(t, err, CodeVersionNotFound)
	}
}

func TestVersionedListAndDeleteMarker(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "a.txt", strings.NewReader("a"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "dir/b.txt", strings.NewReader("b"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "dir/b.txt", strings.NewReader("B"), PutOptions{})

	result, err := s.List(ctx, "abc", ListOptions{Delimiter: "/"})
	testx.RequireNoError(t, err)

	if len(result.Objects) != 1 || result.Objects[0].Key != "a.txt" {
		t.Fatalf("列表对象不符：%+v", result.Objects)
	}
	if len(result.CommonPrefixes) != 1 || result.CommonPrefixes[0] != "dir/" {
		t.Fatalf("公共前缀不符：%v", result.CommonPrefixes)
	}

	// 删除标记后 List 不返回该键
	_ = s.Delete(ctx, "abc", "dir/b.txt")
	result2, _ := s.List(ctx, "abc", ListOptions{})
	if len(result2.Objects) != 1 {
		t.Fatalf("删除标记后列表不符：%+v", result2.Objects)
	}
}

func TestVersioningErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	if _, err := s.GetVersion(ctx, "abc", "k", "", GetOptions{}); err == nil {
		t.Fatal("空版本 ID 应报错")
	}
	if _, err := s.GetVersion(ctx, "abc", "k", "missing", GetOptions{}); err == nil {
		t.Fatal("缺失版本应报错")
	} else {
		mustErrCode(t, err, CodeVersionNotFound)
	}
	if _, err := s.HeadVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本 Head 应报错")
	}
	if err := s.DeleteVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本删除应报错")
	} else {
		mustErrCode(t, err, CodeVersionNotFound)
	}
	if _, err := s.RestoreVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本恢复应报错")
	}
	if _, err := s.ListVersions(ctx, "abc", "missing-key"); err != nil {
		t.Fatalf("无版本键应返回空：%v", err)
	}

	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	info, _ := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Delete(ctx, "abc", "k")
	versions, _ := s.ListVersions(ctx, "abc", "k")
	markerID := versions[0].VersionID
	// 删除标记不可读
	if _, err := s.GetVersion(ctx, "abc", "k", markerID, GetOptions{}); err == nil {
		t.Fatal("删除标记不可读")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}
	// 删除标记的 HeadVersion 返回对象不存在
	if _, err := s.HeadVersion(ctx, "abc", "k", markerID); err == nil {
		t.Fatal("删除标记 HeadVersion 应报错")
	}
	_ = info

	// 损坏版本元数据
	badDir := s.versionDir("abc", "k")
	_ = os.WriteFile(filepath.Join(badDir, "v-bad.json"), []byte("{"), 0o644)
	versions, err := s.ListVersions(ctx, "abc", "k")
	testx.RequireNoError(t, err)

	_ = versions
}

func TestCopyMove(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "src")
	mustBucket(t, s, "dst")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "src", true)
	_, _ = s.Put(ctx, "src", "a.txt", strings.NewReader("hello"),
		PutOptions{ContentType: "text/plain", Metadata: map[string]string{"m": "1"}})
	_, _ = s.Put(ctx, "src", "a.txt", strings.NewReader("HELLO"),
		PutOptions{ContentType: "text/plain", Metadata: map[string]string{"m": "1"}})

	info, err := s.Copy(ctx, "src", "a.txt", "dst", "b.txt")
	testx.RequireNoError(t, err)

	if info.ETag != sha256Hex("HELLO") || info.ContentType != "text/plain" || info.Metadata["m"] != "1" {
		t.Fatalf("复制元数据不符：%+v", info)
	}
	moved, err := s.Move(ctx, "src", "a.txt", "dst", "c.txt")
	testx.RequireNoError(t, err)

	if moved.ETag != sha256Hex("HELLO") {
		t.Fatalf("Move 内容不符：%+v", moved)
	}
	if _, err := s.Get(ctx, "src", "a.txt", GetOptions{}); err == nil {
		t.Fatal("Move 后源应不存在")
	}
	if _, err := s.Copy(ctx, "src", "missing", "dst", "x"); err == nil {
		t.Fatal("复制缺失源应报错")
	}
	if _, err := s.Copy(ctx, "src", "a.txt", "missing", "x"); err == nil {
		t.Fatal("复制到缺失桶应报错")
	}
}

func TestQuota(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketQuota(ctx, "abc", 5)
	_, err := s.Put(ctx, "abc", "a", strings.NewReader("hello"), PutOptions{})
	testx.RequireNoError(t, err)

	if _, err := s.Put(ctx, "abc", "b", strings.NewReader("world"), PutOptions{}); err == nil {
		t.Fatal("超配额应报错")
	} else {
		mustErrCode(t, err, CodeQuotaExceeded)
	}
	if _, err := s.Head(ctx, "abc", "b"); err == nil {
		t.Fatal("超配额对象应回滚")
	}
	if _, err := s.Head(ctx, "abc", "a"); err != nil {
		t.Fatalf("配额内对象应保留：%v", err)
	}

	// 版本化桶配额回滚
	s2, _ := newStore(t)
	mustBucket(t, s2, "abc")
	_, _ = s2.SetBucketVersioning(ctx, "abc", true)
	_, _ = s2.SetBucketQuota(ctx, "abc", 5)
	_, _ = s2.Put(ctx, "abc", "k", strings.NewReader("aaaa"), PutOptions{})
	if _, err := s2.Put(ctx, "abc", "k", strings.NewReader("bbb"), PutOptions{}); err == nil {
		t.Fatal("版本化超配额应报错")
	} else {
		mustErrCode(t, err, CodeQuotaExceeded)
	}
	versions, _ := s2.ListVersions(ctx, "abc", "k")
	if len(versions) != 1 {
		t.Fatalf("超配额版本应回滚：%+v", versions)
	}
}

func TestVersioningHelpers(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Delete(ctx, "abc", "k")

	// 软删除后再 Delete：对象不可见 → object_not_found
	if err := s.Delete(ctx, "abc", "k"); err == nil {
		t.Fatal("重复软删除应报错")
	} else {
		mustErrCode(t, err, CodeObjectNotFound)
	}

	// 新版本 ID 回退分支
	old := versionRand
	versionRand = func(n int) ([]byte, error) { return nil, errors.New("随机数失败") }
	defer func() { versionRand = old }()
	id := newVersionID()
	if !strings.HasPrefix(id, "v-") {
		t.Fatalf("回退版本 ID 不符：%s", id)
	}
}

func TestVersioningLegacyFallback(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "legacy", strings.NewReader("old"), PutOptions{})
	_, _ = s.SetBucketVersioning(ctx, "abc", true)

	obj, err := s.Get(ctx, "abc", "legacy", GetOptions{})
	testx.RequireNoError(t, err)

	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	if string(data) != "old" {
		t.Fatalf("旧对象内容不符：%s", data)
	}
	result, _ := s.List(ctx, "abc", ListOptions{})
	if len(result.Objects) != 1 || result.Objects[0].Key != "legacy" {
		t.Fatalf("旧对象应出现在列表：%+v", result.Objects)
	}

	_, _ = s.Put(ctx, "abc", "legacy", strings.NewReader("new"), PutOptions{})
	result2, _ := s.List(ctx, "abc", ListOptions{})
	if len(result2.Objects) != 1 || result2.Objects[0].Key != "legacy" {
		t.Fatalf("版本化后列表应取最新版本：%+v", result2.Objects)
	}
	obj2, _ := s.Get(ctx, "abc", "legacy", GetOptions{})
	data2, _ := io.ReadAll(obj2)
	_ = obj2.Close()
	if string(data2) != "new" {
		t.Fatalf("新版本内容不符：%s", data2)
	}
	_ = s.Delete(ctx, "abc", "legacy")
	if _, err := s.Get(ctx, "abc", "legacy", GetOptions{}); err == nil {
		t.Fatal("删除标记后旧对象不可见")
	}
	versions, _ := s.ListVersions(ctx, "abc", "legacy")
	if err := s.DeleteVersion(ctx, "abc", "legacy", versions[0].VersionID); err != nil {
		t.Fatal(err)
	}
	obj3, err := s.Get(ctx, "abc", "legacy", GetOptions{})
	testx.RequireNoError(t, err)

	data3, _ := io.ReadAll(obj3)
	_ = obj3.Close()
	if string(data3) != "new" {
		t.Fatalf("删除标记移除后内容不符：%s", data3)
	}
	versions, _ = s.ListVersions(ctx, "abc", "legacy")
	if err := s.DeleteVersion(ctx, "abc", "legacy", versions[0].VersionID); err != nil {
		t.Fatal(err)
	}
	obj3, err = s.Get(ctx, "abc", "legacy", GetOptions{})
	testx.RequireNoError(t, err)

	data3, _ = io.ReadAll(obj3)
	_ = obj3.Close()
	if string(data3) != "old" {
		t.Fatalf("回退内容不符：%s", data3)
	}
}
