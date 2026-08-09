package filex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func testKey() []byte {
	return bytes.Repeat([]byte{0x42}, 32)
}

func TestEncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, EncryptionKey: testKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mustBucket(t, s, "abc")
	ctx := context.Background()

	info, err := s.Put(ctx, "abc", "k", strings.NewReader("secret data"), PutOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("加密写入失败：%v", err)
	}
	if info.ETag != sha256Hex("secret data") {
		t.Fatalf("ETag 应为明文哈希：%s", info.ETag)
	}
	meta, _ := readObjectMeta(s.fs, s.objectMetaPath("abc", "k"))
	if meta.Encryption == nil || meta.Encryption.Algorithm != encryptionAlgorithm {
		t.Fatalf("加密元数据缺失：%+v", meta.Encryption)
	}
	raw, _ := os.ReadFile(s.objectDataPath("abc", "k"))
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal("数据文件不应包含明文")
	}
	if len(raw) != len("secret data")+16 {
		t.Fatalf("密文长度不符：%d", len(raw))
	}

	obj, err := s.Get(ctx, "abc", "k", GetOptions{Verify: true})
	if err != nil {
		t.Fatalf("解密读取失败：%v", err)
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != "secret data" {
		t.Fatalf("解密内容不符：%q, %v", data, err)
	}
	objPlain, err := s.Get(ctx, "abc", "k", GetOptions{})
	if err != nil {
		t.Fatalf("非校验解密读取失败：%v", err)
	}
	dataPlain, _ := io.ReadAll(objPlain)
	_ = objPlain.Close()
	if string(dataPlain) != "secret data" {
		t.Fatalf("非校验解密内容不符：%s", dataPlain)
	}

	if _, err := s.Get(ctx, "abc", "k", GetOptions{Range: &ByteRange{Start: 0, End: 1}}); err == nil {
		t.Fatal("加密对象范围读取应被拒绝")
	} else {
		mustErrCode(t, err, CodeInvalidArgument)
	}

	// 篡改密文后校验失败
	_ = os.WriteFile(s.objectDataPath("abc", "k"), append([]byte{0x00}, raw[1:]...), 0o644)
	obj2, err := s.Get(ctx, "abc", "k", GetOptions{Verify: true})
	if err != nil {
		t.Fatalf("篡改后打开失败：%v", err)
	}
	defer obj2.Close()
	if _, err := io.ReadAll(obj2); err == nil {
		t.Fatal("篡改密文校验应失败")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
}

func TestEncryptionWrongKey(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(Config{DataDir: dir, EncryptionKey: testKey()})
	mustBucket(t, s, "abc")
	_, _ = s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Close()

	s2, err := New(Config{DataDir: dir, EncryptionKey: bytes.Repeat([]byte{0x24}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.Get(context.Background(), "abc", "k", GetOptions{}); err == nil {
		t.Fatal("错误主密钥应解包失败")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}
}

func TestEncryptionMissingKey(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(Config{DataDir: dir, EncryptionKey: testKey()})
	mustBucket(t, s, "abc")
	_, _ = s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Close()

	s2, _ := New(Config{DataDir: dir})
	defer s2.Close()
	if _, err := s2.Get(context.Background(), "abc", "k", GetOptions{}); err == nil {
		t.Fatal("缺少主密钥应报错")
	} else {
		mustErrCode(t, err, CodeStorageFailed)
	}
}

func TestEncryptionMultipart(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "big", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "big", up.UploadID, 1, strings.NewReader("aaa"))
	_, _ = s.UploadPart(ctx, "abc", "big", up.UploadID, 2, strings.NewReader("bbb"))
	info, err := s.CompleteMultipartUpload(ctx, "abc", "big", up.UploadID)
	if err != nil {
		t.Fatalf("加密分片完成失败：%v", err)
	}
	if info.ETag != sha256Hex("aaabbb") {
		t.Fatalf("ETag 不符：%s", info.ETag)
	}
	obj, err := s.Get(ctx, "abc", "big", GetOptions{Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	if string(data) != "aaabbb" {
		t.Fatalf("解密分片内容不符：%s", data)
	}
}

func TestNewInvalidEncryptionKey(t *testing.T) {
	if _, err := New(Config{DataDir: t.TempDir(), EncryptionKey: make([]byte, 16)}); err == nil {
		t.Fatal("16 字节主密钥应报错")
	} else {
		mustErrCode(t, err, CodeInvalidConfig)
	}
}

func TestUnwrapObjectKeyErrors(t *testing.T) {
	kek := testKey()
	c, err := newObjectCipher(kek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unwrapObjectKey(nil, c.meta); err == nil {
		t.Fatal("缺少主密钥应报错")
	}
	badMeta := c.meta
	badMeta.WrappedKey = "!!!"
	if _, err := unwrapObjectKey(kek, badMeta); err == nil {
		t.Fatal("坏 base64 应报错")
	}
	badMeta = c.meta
	badMeta.KeyNonce = "zz"
	if _, err := unwrapObjectKey(kek, badMeta); err == nil {
		t.Fatal("坏十六进制应报错")
	}
	if _, err := unwrapObjectKey([]byte{1, 2, 3}, c.meta); err == nil {
		t.Fatal("非法主密钥长度应报错")
	}
	if _, err := unwrapObjectKey(bytes.Repeat([]byte{0x11}, 32), c.meta); err == nil {
		t.Fatal("错误主密钥应解包失败")
	}
	dek, err := unwrapObjectKey(kek, c.meta)
	if err != nil || len(dek) != 32 {
		t.Fatalf("正常解包失败：%d, %v", len(dek), err)
	}
}

type partialRand struct {
	remaining int
}

func (p *partialRand) Read(b []byte) (int, error) {
	if p.remaining <= 0 {
		return 0, errors.New("随机数耗尽")
	}
	n := len(b)
	if n > p.remaining {
		n = p.remaining
	}
	for i := 0; i < n; i++ {
		b[i] = 0x5a
	}
	p.remaining -= n
	return n, nil
}

func TestNewObjectCipherRandErrors(t *testing.T) {
	old := cryptoRand
	defer func() { cryptoRand = old }()
	cryptoRand = &partialRand{remaining: 0}
	if _, err := newObjectCipher(testKey()); err == nil {
		t.Fatal("DEK 随机数失败应报错")
	}
	cryptoRand = &partialRand{remaining: 32}
	if _, err := newObjectCipher(testKey()); err == nil {
		t.Fatal("密钥随机数失败应报错")
	}
	cryptoRand = &partialRand{remaining: 44}
	if _, err := newObjectCipher(testKey()); err == nil {
		t.Fatal("数据随机数失败应报错")
	}
	cryptoRand = old
	if _, err := newObjectCipher([]byte{1, 2, 3}); err == nil {
		t.Fatal("非法主密钥长度应报错")
	}
}

func newStoreWithKey(t *testing.T, key []byte) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func TestPutEncryptionWriteError(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return f, nil
	}
	if _, err := s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("加密随机数写入失败应报错")
	}
}

func TestCompleteEncryptionWriteError(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("a"))
	s.fs.CreateTemp = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return f, nil
	}
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); err == nil {
		t.Fatal("加密随机数写入失败应报错")
	}
}

func TestOpenObjectNonceReadError(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = os.WriteFile(s.objectDataPath("abc", "k"), []byte("short"), 0o644)
	if _, err := s.Get(ctx, "abc", "k", GetOptions{}); err == nil {
		t.Fatal("加密随机数不足应报错")
	} else {
		mustErrCode(t, err, CodeMetadataCorrupt)
	}
}

func TestPutCipherGenerationError(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	old := cryptoRand
	cryptoRand = failReader{}
	defer func() { cryptoRand = old }()
	if _, err := s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{}); err == nil {
		t.Fatal("加密上下文生成失败应报错")
	}
}

func TestCompleteCipherGenerationError(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "k", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "k", up.UploadID, 1, strings.NewReader("a"))
	old := cryptoRand
	cryptoRand = failReader{}
	defer func() { cryptoRand = old }()
	if _, err := s.CompleteMultipartUpload(ctx, "abc", "k", up.UploadID); err == nil {
		t.Fatal("加密上下文生成失败应报错")
	}
}
