package filex

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultipartLifecycle(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	up, err := s.InitiateMultipartUpload(ctx, "abc", "big/file.bin", PutOptions{
		ContentType: "application/octet-stream",
		Metadata:    map[string]string{"multipart": "yes"},
	})
	if err != nil {
		t.Fatalf("创建分片会话失败：%v", err)
	}
	if up.UploadID == "" || up.Bucket != "abc" || up.Key != "big/file.bin" {
		t.Fatalf("会话信息不符：%+v", up)
	}

	// 乱序上传部件
	if _, err := s.UploadPart(ctx, "abc", "big/file.bin", up.UploadID, 2, strings.NewReader("bbb")); err != nil {
		t.Fatal(err)
	}
	p1, err := s.UploadPart(ctx, "abc", "big/file.bin", up.UploadID, 1, strings.NewReader("aaa"))
	if err != nil {
		t.Fatal(err)
	}
	if p1.PartNumber != 1 || p1.Size != 3 || p1.SHA256 != sha256Hex("aaa") {
		t.Fatalf("部件信息不符：%+v", p1)
	}
	// 覆盖部件 2
	if _, err := s.UploadPart(ctx, "abc", "big/file.bin", up.UploadID, 2, strings.NewReader("BBB")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart(ctx, "abc", "big/file.bin", up.UploadID, 3, strings.NewReader("ccc")); err != nil {
		t.Fatal(err)
	}

	parts, err := s.ListParts(ctx, "abc", "big/file.bin", up.UploadID)
	if err != nil {
		t.Fatalf("ListParts 失败：%v", err)
	}
	if len(parts) != 3 || parts[0].PartNumber != 1 || parts[1].PartNumber != 2 || parts[1].Size != 3 {
		t.Fatalf("部件列表不符：%+v", parts)
	}

	info, err := s.CompleteMultipartUpload(ctx, "abc", "big/file.bin", up.UploadID)
	if err != nil {
		t.Fatalf("完成分片上传失败：%v", err)
	}
	want := "aaaBBBccc"
	if info.ETag != sha256Hex(want) || info.Size != int64(len(want)) {
		t.Fatalf("合并对象信息不符：%+v", info)
	}
	if info.Metadata["multipart"] != "yes" {
		t.Fatalf("元数据未保留：%+v", info.Metadata)
	}
	obj, err := s.Get(ctx, "abc", "big/file.bin", GetOptions{Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != want {
		t.Fatalf("合并内容不符：%q, %v", data, err)
	}
	if _, err := os.Stat(s.uploadDir("abc", up.UploadID)); !os.IsNotExist(err) {
		t.Fatal("上传会话应已清理")
	}
}

func TestMultipartAbort(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	up, err := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v"))
	if err := s.AbortMultipartUpload(ctx, "abc", "k", up.UploadID); err != nil {
		t.Fatalf("中止失败：%v", err)
	}
	if err := s.AbortMultipartUpload(ctx, "abc", "k", up.UploadID); err == nil {
		t.Fatal("重复中止应报错")
	} else {
		mustErrCode(t, err, CodeUploadNotFound)
	}
}

func TestMultipartErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()

	if _, err := s.InitiateMultipartUpload(ctx, "BAD", "k", PutOptions{}); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.InitiateMultipartUpload(ctx, "abc", "", PutOptions{}); err == nil {
		t.Fatal("非法键应报错")
	}
	if _, err := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{Metadata: map[string]string{"": "v"}}); err == nil {
		t.Fatal("非法元数据应报错")
	}
	if _, err := s.InitiateMultipartUpload(ctx, "missing", "k", PutOptions{}); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.CompleteMultipartUpload(ctx, "BAD", "k", "u"); err == nil {
		t.Fatal("Complete 非法桶名应报错")
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "", "u"); err == nil {
		t.Fatal("Complete 非法键应报错")
	}
	if _, err := s.CompleteMultipartUpload(ctx, "missing", "k", "u"); err == nil {
		t.Fatal("Complete 缺失桶应报错")
	}
	if err := s.AbortMultipartUpload(ctx, "BAD", "k", "u"); err == nil {
		t.Fatal("Abort 非法桶名应报错")
	}
	if err := s.AbortMultipartUpload(ctx, "abc", "", "u"); err == nil {
		t.Fatal("Abort 非法键应报错")
	}
	if err := s.AbortMultipartUpload(ctx, "missing", "k", "u"); err == nil {
		t.Fatal("Abort 缺失桶应报错")
	}
	if _, err := s.ListParts(ctx, "BAD", "k", "u"); err == nil {
		t.Fatal("ListParts 非法桶名应报错")
	}
	if _, err := s.ListParts(ctx, "abc", "", "u"); err == nil {
		t.Fatal("ListParts 非法键应报错")
	}
	if _, err := s.ListParts(ctx, "missing", "k", "u"); err == nil {
		t.Fatal("ListParts 缺失桶应报错")
	}

	up, err := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 0, strings.NewReader("v")); err == nil {
		t.Fatal("部件号 0 应报错")
	}
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, maxUploadParts+1, strings.NewReader("v")); err == nil {
		t.Fatal("部件号超限应报错")
	}
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, nil); err == nil {
		t.Fatal("nil 读取器应报错")
	}
	if _, err := s.UploadPart(ctx, "abc", "", up.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("非法键应报错")
	}
	if _, err := s.UploadPart(ctx, "BAD", "k", up.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if _, err := s.UploadPart(ctx, "abc", "k", "", 1, strings.NewReader("v")); err == nil {
		t.Fatal("空会话 ID 应报错")
	}
	if _, err := s.UploadPart(ctx, "missing", "k", up.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.UploadPart(ctx, "abc", "k", "missing", 1, strings.NewReader("v")); err == nil {
		t.Fatal("缺失会话应报错")
	} else {
		mustErrCode(t, err, CodeUploadNotFound)
	}
	if _, err := s.UploadPart(ctx, "abc", "other", up.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("会话键不匹配应报错")
	} else {
		mustErrCode(t, err, CodeUploadInvalid)
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "other", up.UploadID); err == nil {
		t.Fatal("Complete 会话键不匹配应报错")
	} else {
		mustErrCode(t, err, CodeUploadInvalid)
	}

	// 损坏会话元数据
	up2, _ := s.InitiateMultipartUpload(ctx, "abc", "k2", PutOptions{})
	_ = os.WriteFile(s.uploadMetaPath("abc", up2.UploadID), []byte("{"), 0o644)
	if _, err := s.UploadPart(ctx, "abc", "k2", up2.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("损坏会话元数据应报错")
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); err == nil {
		t.Fatal("Complete 损坏会话元数据应报错")
	}
	if _, err := s.ListParts(ctx, "abc", "k2", up2.UploadID); err == nil {
		t.Fatal("ListParts 损坏会话元数据应报错")
	}

	// 会话目录不存在时 Complete/ListParts
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失会话 Complete 应报错")
	} else {
		mustErrCode(t, err, CodeUploadNotFound)
	}
	if _, err := s.ListParts(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失会话 ListParts 应报错")
	} else {
		mustErrCode(t, err, CodeUploadNotFound)
	}

	// 空会话 Complete
	up3, _ := s.InitiateMultipartUpload(ctx, "abc", "k3", PutOptions{})
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k3", up3.UploadID); err == nil {
		t.Fatal("空会话 Complete 应报错")
	} else {
		mustErrCode(t, err, CodeUploadIncomplete)
	}

	// 不连续部件
	up4, _ := s.InitiateMultipartUpload(ctx, "abc", "k4", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k4", up4.UploadID, 1, strings.NewReader("a"))
	_, _ = s.UploadPart(ctx, "abc", "k4", up4.UploadID, 3, strings.NewReader("c"))
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k4", up4.UploadID); err == nil {
		t.Fatal("不连续部件应报错")
	} else {
		mustErrCode(t, err, CodeUploadIncomplete)
	}

	// 部件数据缺失
	up5, _ := s.InitiateMultipartUpload(ctx, "abc", "k5", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k5", up5.UploadID, 1, strings.NewReader("a"))
	_ = os.Remove(s.partDataPath("abc", up5.UploadID, 1))
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k5", up5.UploadID); err == nil {
		t.Fatal("部件数据缺失应报错")
	} else {
		mustErrCode(t, err, CodeUploadIncomplete)
	}

	// 部件元数据缺失
	up6, _ := s.InitiateMultipartUpload(ctx, "abc", "k6", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k6", up6.UploadID, 1, strings.NewReader("a"))
	_ = os.Remove(s.partMetaPath("abc", up6.UploadID, 1))
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k6", up6.UploadID); err == nil {
		t.Fatal("部件元数据缺失应报错")
	} else {
		mustErrCode(t, err, CodeUploadIncomplete)
	}
}

func TestMultipartIOErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("CreateTemp 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.WriteToFile = func(io.Writer, io.Reader) (int64, error) { return 0, injected }
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("WriteToFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.SyncFile = func(*os.File) error { return injected }
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("SyncFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CloseFile = func(f *os.File) error { _ = f.Close(); return injected }
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("CloseFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.Rename = func(string, string) error { return injected }
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("Rename 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		if strings.Contains(pattern, ".meta-") {
			return nil, injected
		}
		return os.CreateTemp(dir, pattern)
	}
	if _, err := s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("v")); !errors.Is(err, injected) {
		t.Fatalf("部件元数据 CreateTemp 失败应透传：%v", err)
	}

	// 部件超限
	small, _ := New(Config{DataDir: t.TempDir(), MaxObjectSize: 2})
	defer small.Close()
	mustBucket(t, small, "abc")
	smallUp, _ := small.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	if _, err := small.UploadPart(ctx, "abc", "k", smallUp.UploadID, 1, strings.NewReader("too-long")); err == nil {
		t.Fatal("超限部件应报错")
	} else {
		mustErrCode(t, err, CodeObjectTooLarge)
	}
}

func TestMultipartCompleteIOErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("a"))

	s.fs.MkdirAll = func(string, os.FileMode) error { return injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); !errors.Is(err, injected) {
		t.Fatalf("MkdirAll 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		if strings.Contains(pattern, ".tmp-") {
			return nil, injected
		}
		return os.CreateTemp(dir, pattern)
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); !errors.Is(err, injected) {
		t.Fatalf("合并 CreateTemp 失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); !errors.Is(err, injected) {
		t.Fatalf("ReadDir 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	_ = os.WriteFile(filepath.Join(s.uploadDir("abc", up.UploadID), "part-abc.json"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(s.uploadDir("abc", up.UploadID), "part-1.json"), []byte("{"), 0o644)
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); err == nil {
		t.Fatal("损坏部件元数据应报错")
	}

	// 恢复有效部件元数据
	up2, _ := s.InitiateMultipartUpload(ctx, "abc", "k2", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k2", up2.UploadID, 1, strings.NewReader("a"))
	s.fs = defaultFSOps
	s.fs.OpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("OpenFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.WriteToFile = func(io.Writer, io.Reader) (int64, error) { return 0, injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("合并 WriteToFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.SyncFile = func(*os.File) error { return injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("合并 SyncFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.CloseFile = func(f *os.File) error { _ = f.Close(); return injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("合并 CloseFile 失败应透传：%v", err)
	}
	s.fs = defaultFSOps
	s.fs.Rename = func(string, string) error { return injected }
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("合并 Rename 失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	// 部件大小与元数据不符
	up3, _ := s.InitiateMultipartUpload(ctx, "abc", "k3", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k3", up3.UploadID, 1, strings.NewReader("aaa"))
	_ = os.WriteFile(s.partMetaPath("abc", up3.UploadID, 1), []byte(`{"part_number":1,"size":99,"sha256":"`+strings.Repeat("a", 64)+`"}`), 0o644)
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k3", up3.UploadID); err == nil {
		t.Fatal("部件大小不符应报错")
	}

	// 元数据写入失败
	up4, _ := s.InitiateMultipartUpload(ctx, "abc", "k4", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k4", up4.UploadID, 1, strings.NewReader("a"))
	s.fs = defaultFSOps
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		if strings.Contains(pattern, ".meta-") {
			return nil, injected
		}
		return os.CreateTemp(dir, pattern)
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k4", up4.UploadID); !errors.Is(err, injected) {
		t.Fatalf("对象元数据写入失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	// 成功完成但会话清理失败：仍返回成功并告警
	store2, _ := newStore(t)
	mustBucket(t, store2, "abc")
	upWarn, _ := store2.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	_, _ = store2.UploadPart(ctx, "abc", "k", upWarn.UploadID, 1, strings.NewReader("a"))
	store2.fs.RemoveAll = func(string) error { return injected }
	if _, err := store2.CompleteMultipartUpload(ctx, "abc", "k", upWarn.UploadID); err != nil {
		t.Fatalf("清理失败不应影响完成：%v", err)
	}

	// 合并后总大小超限
	small, _ := New(Config{DataDir: t.TempDir(), MaxObjectSize: 2})
	defer small.Close()
	mustBucket(t, small, "abc")
	up5, _ := small.InitiateMultipartUpload(ctx, "abc", "k5", PutOptions{})
	_, _ = small.UploadPart(ctx, "abc", "k5", up5.UploadID, 1, strings.NewReader("aa"))
	_, _ = small.UploadPart(ctx, "abc", "k5", up5.UploadID, 2, strings.NewReader("bb"))
	if _, err := small.CompleteMultipartUpload(ctx, "abc", "k5", up5.UploadID); err == nil {
		t.Fatal("合并超限应报错")
	} else {
		mustErrCode(t, err, CodeObjectTooLarge)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("随机数失败") }

func TestMultipartAbortAndListErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	_ = os.WriteFile(s.uploadMetaPath("abc", up.UploadID), []byte("{"), 0o644)
	if err := s.AbortMultipartUpload(ctx, "abc", "k", up.UploadID); err == nil {
		t.Fatal("Abort 损坏会话元数据应报错")
	}
	up, _ = s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	s.fs.RemoveAll = func(string) error { return injected }
	if err := s.AbortMultipartUpload(ctx, "abc", "k", up.UploadID); !errors.Is(err, injected) {
		t.Fatalf("Abort RemoveAll 失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	up2, _ := s.InitiateMultipartUpload(ctx, "abc", "k2", PutOptions{})
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.ListParts(ctx, "abc", "k2", up2.UploadID); !errors.Is(err, injected) {
		t.Fatalf("ListParts ReadDir 失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	// ListParts 跳过损坏部件元数据
	up3, _ := s.InitiateMultipartUpload(ctx, "abc", "k3", PutOptions{})
	_ = os.WriteFile(s.partMetaPath("abc", up3.UploadID, 1), []byte("{"), 0o644)
	parts, err := s.ListParts(ctx, "abc", "k3", up3.UploadID)
	if err != nil {
		t.Fatalf("损坏部件应跳过：%v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("损坏部件不应返回：%+v", parts)
	}
}

func TestMultipartInitiateWriteError(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	injected := errors.New("注入错误")
	s.fs.CreateTemp = func(string, string) (*os.File, error) { return nil, injected }
	if _, err := s.InitiateMultipartUpload(context.Background(), "abc", "k", PutOptions{}); !errors.Is(err, injected) {
		t.Fatalf("Initiate 元数据写入失败应透传：%v", err)
	}
}

func TestNewUploadID(t *testing.T) {
	id := newUploadID()
	if len(id) != 24 {
		t.Fatalf("上传 ID 长度不符：%s", id)
	}
	old := uploadRand
	uploadRand = failReader{}
	defer func() { uploadRand = old }()
	fallback := newUploadID()
	if !strings.HasPrefix(fallback, "u-") {
		t.Fatalf("回退上传 ID 不符：%s", fallback)
	}
}

func TestReadPartMetaMissing(t *testing.T) {
	if _, err := readPartMeta(defaultFSOps, filepath.Join(t.TempDir(), "missing.json")); !os.IsNotExist(err) {
		t.Fatalf("缺失部件元数据应返回不存在：%v", err)
	}
}
