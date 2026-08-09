package filex

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestVerifyObject(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("hello"), PutOptions{})
	if err := s.VerifyObject(ctx, "abc", "k"); err != nil {
		t.Fatalf("完整性校验失败：%v", err)
	}
	dataPath := s.objectDataPath("abc", "k")
	data, _ := os.ReadFile(dataPath)
	data[0] ^= 0xff
	_ = os.WriteFile(dataPath, data, 0o644)
	if err := s.VerifyObject(ctx, "abc", "k"); err == nil {
		t.Fatal("篡改内容应校验失败")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
	if err := s.VerifyObject(ctx, "abc", "missing"); err == nil {
		t.Fatal("缺失对象应报错")
	}
	if err := s.VerifyObject(ctx, "BAD", "k"); err == nil {
		t.Fatal("非法桶名应报错")
	}
	if err := s.VerifyObject(ctx, "abc", ""); err == nil {
		t.Fatal("非法键应报错")
	}
}

func TestVerifyAll(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	mustBucket(t, s, "vbc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "good", strings.NewReader("g"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "bad", strings.NewReader("b"), PutOptions{})
	data, _ := os.ReadFile(s.objectDataPath("abc", "bad"))
	data[0] ^= 0xff
	_ = os.WriteFile(s.objectDataPath("abc", "bad"), data, 0o644)

	_, _ = s.SetBucketVersioning(ctx, "vbc", true)
	_, _ = s.Put(ctx, "vbc", "k", strings.NewReader("v1"), PutOptions{})
	_, _ = s.Put(ctx, "vbc", "k", strings.NewReader("v2"), PutOptions{})
	_ = s.Delete(ctx, "vbc", "k")

	report, err := s.VerifyAll(ctx, 0)
	if err != nil {
		t.Fatalf("VerifyAll 失败：%v", err)
	}
	if report.Scanned != 4 || report.Corrupt != 1 {
		t.Fatalf("审计报告不符：%+v", report)
	}

	report2, err := s.VerifyAll(ctx, 100)
	if err != nil || report2.Scanned != 4 {
		t.Fatalf("并发上限审计不符：%+v, %v", report2, err)
	}
}

func TestVerifyAllEncrypted(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	_, _ = s.Put(context.Background(), "abc", "k", strings.NewReader("secret"), PutOptions{})
	report, err := s.VerifyAll(context.Background(), 2)
	if err != nil {
		t.Fatalf("加密对象审计失败：%v", err)
	}
	if report.Scanned != 1 || report.Corrupt != 0 {
		t.Fatalf("加密对象审计报告不符：%+v", report)
	}
}

func TestBucketUsagePublic(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "a", strings.NewReader("hello"), PutOptions{})
	usage, err := s.BucketUsage(ctx, "abc")
	if err != nil || usage != 5 {
		t.Fatalf("桶用量不符：%d, %v", usage, err)
	}
	if _, err := s.BucketUsage(ctx, "missing"); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.BucketUsage(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}
}

func TestContextCancellation(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := s.Put(cancelCtx, "abc", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("取消上下文 Put 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}

	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k2", PutOptions{})
	if _, err := s.UploadPart(cancelCtx, "abc", "k2", up.UploadID, 1, strings.NewReader("v")); err == nil {
		t.Fatal("取消上下文 UploadPart 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}
	_, _ = s.UploadPart(ctx, "abc", "k2", up.UploadID, 1, strings.NewReader("v"))
	if _, err := s.CompleteMultipartUpload(cancelCtx, "abc", "k2", up.UploadID); err == nil {
		t.Fatal("取消上下文 Complete 应报错")
	} else {
		mustErrCode(t, err, CodeCancelled)
	}

	if err := wrapCtxErr(context.DeadlineExceeded); err == nil {
		t.Fatal("DeadlineExceeded 应归一为取消错误")
	}
	if err := wrapCtxErr(errors.New("普通错误")); err == nil {
		t.Fatal("普通错误应原样返回")
	}
}


func TestVerifyAllErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	injected := errors.New("注入错误")

	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.VerifyAll(ctx, 2); !errors.Is(err, injected) {
		t.Fatalf("桶枚举失败应透传：%v", err)
	}
	s.fs = defaultFSOps

	s.fs.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == s.objectsDir("abc") {
			return nil, injected
		}
		return os.ReadDir(path)
	}
	report, err := s.VerifyAll(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("对象枚举失败应记录：%+v", report)
	}
}
