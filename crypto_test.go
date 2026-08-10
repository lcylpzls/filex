package filex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	testx "github.com/lcylpzls/testx"
)

func testKey() []byte {
	return bytes.Repeat([]byte{0x42}, 32)
}

func TestEncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, EncryptionKey: testKey()})
	testx.RequireNoError(t, err)

	defer s.Close()
	mustBucket(t, s, "abc")
	ctx := context.Background()

	info, err := s.Put(ctx, "abc", "k", strings.NewReader("secret data"), PutOptions{
		ContentType: "text/plain",
	})
	testx.RequireNoError(t, err)

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
	if len(raw) <= len("secret data") {
		t.Fatalf("cryptox 流密文长度异常：%d", len(raw))
	}

	obj, err := s.Get(ctx, "abc", "k", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != "secret data" {
		t.Fatalf("解密内容不符：%q, %v", data, err)
	}
	objPlain, err := s.Get(ctx, "abc", "k", GetOptions{})
	testx.RequireNoError(t, err)

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
	tampered := append([]byte(nil), raw...)
	tampered[0] ^= 0xff // 破坏 cryptox 流标识
	_ = os.WriteFile(s.objectDataPath("abc", "k"), tampered, 0o644)
	obj2, err := s.Get(ctx, "abc", "k", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	defer obj2.Close()
	if _, err := io.ReadAll(obj2); err == nil {
		t.Fatal("篡改密文校验应失败")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
	// 认证失败后再次读取应返回同一错误
	if _, err := obj2.Read(make([]byte, 1)); err == nil {
		t.Fatal("认证失败后再次读取应报错")
	}
}

func TestEncryptionWrongKey(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(Config{DataDir: dir, EncryptionKey: testKey()})
	mustBucket(t, s, "abc")
	_, _ = s.Put(context.Background(), "abc", "k", strings.NewReader("v"), PutOptions{})
	_ = s.Close()

	s2, err := New(Config{DataDir: dir, EncryptionKey: bytes.Repeat([]byte{0x24}, 32)})
	testx.RequireNoError(t, err)

	defer s2.Close()
	obj, err := s2.Get(context.Background(), "abc", "k", GetOptions{})
	testx.RequireNoError(t, err)
	_, err = io.ReadAll(obj)
	_ = obj.Close()
	if err == nil {
		t.Fatal("错误主密钥应解密失败")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
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
	testx.RequireNoError(t, err)

	if info.ETag != sha256Hex("aaabbb") {
		t.Fatalf("ETag 不符：%s", info.ETag)
	}
	obj, err := s.Get(ctx, "abc", "big", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	if string(data) != "aaabbb" {
		t.Fatalf("解密分片内容不符：%s", data)
	}
}

func TestNewInvalidEncryptionKey(t *testing.T) {
	if _, err := New(Config{DataDir: t.TempDir(), EncryptionKey: make([]byte, 15)}); err == nil {
		t.Fatal("15 字节主密钥应报错")
	} else {
		mustErrCode(t, err, CodeInvalidConfig)
	}
}

func TestNewObjectCipherValidation(t *testing.T) {
	if c := newObjectCipher(nil); c != nil {
		t.Fatalf("无主密钥应返回 nil：%v", c)
	}
	for _, n := range []int{16, 24, 32} {
		if c := newObjectCipher(make([]byte, n)); c == nil || !c.enabled {
			t.Fatalf("%d 字节主密钥应启用加密", n)
		}
	}
}

func TestEncryptPipeRoundTrip(t *testing.T) {
	kek := testKey()
	var buf bytes.Buffer
	plain, finish := encryptPipe(kek, &buf)
	_, _ = plain.Write([]byte("hello world"))
	testx.RequireNoError(t, finish())
	if bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Fatal("密文不应包含明文")
	}
	dec := decryptReader(kek, bytes.NewReader(buf.Bytes()))
	data, err := io.ReadAll(dec)
	if err != nil || string(data) != "hello world" {
		t.Fatalf("解密不符：%q, %v", data, err)
	}
}

func TestEncryptPipeWriteError(t *testing.T) {
	plain, finish := encryptPipe(testKey(), failWriter{})
	_, _ = plain.Write([]byte("data"))
	if err := finish(); err == nil {
		t.Fatal("目标写入失败应报错")
	}
}

func TestDecryptReaderTampered(t *testing.T) {
	kek := testKey()
	var buf bytes.Buffer
	plain, finish := encryptPipe(kek, &buf)
	_, _ = plain.Write([]byte("secret"))
	testx.RequireNoError(t, finish())
	tampered := append([]byte(nil), buf.Bytes()...)
	tampered[len(tampered)-1] ^= 0xff
	dec := decryptReader(kek, bytes.NewReader(tampered))
	if _, err := io.ReadAll(dec); err == nil {
		t.Fatal("篡改密文应认证失败")
	} else {
		mustErrCode(t, err, CodeChecksumMismatch)
	}
}

func TestDecryptReaderTruncated(t *testing.T) {
	kek := testKey()
	var buf bytes.Buffer
	plain, finish := encryptPipe(kek, &buf)
	_, _ = plain.Write([]byte("secret"))
	testx.RequireNoError(t, finish())
	dec := decryptReader(kek, bytes.NewReader(buf.Bytes()[:10]))
	if _, err := io.ReadAll(dec); err == nil {
		t.Fatal("截断密文应报错")
	}
}

func TestEncryptionChunkBoundaries(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	const chunk = 64 * 1024
	big := strings.Repeat("x", chunk*2+123)
	_, err := s.Put(ctx, "abc", "big", strings.NewReader(big), PutOptions{})
	testx.RequireNoError(t, err)

	obj, err := s.Get(ctx, "abc", "big", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != big {
		t.Fatalf("多块解密不符：%d 字节, %v", len(data), err)
	}
	// 空对象
	if _, err := s.Put(ctx, "abc", "empty", strings.NewReader(""), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	obj2, err := s.Get(ctx, "abc", "empty", GetOptions{Verify: true})
	testx.RequireNoError(t, err)

	data2, _ := io.ReadAll(obj2)
	_ = obj2.Close()
	if len(data2) != 0 {
		t.Fatalf("空对象解密不符：%d", len(data2))
	}
}

func newStoreWithKey(t *testing.T, key []byte) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, EncryptionKey: key})
	testx.RequireNoError(t, err)

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
		t.Fatal("加密输出写入失败应报错")
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
		t.Fatal("加密输出写入失败应报错")
	}
}

func TestOpenObjectEncryptionMetaErrors(t *testing.T) {
	s, _ := newStoreWithKey(t, testKey())
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})

	// 密文截断：读取过程报错
	_ = os.WriteFile(s.objectDataPath("abc", "k"), []byte("short"), 0o644)
	obj, err := s.Get(ctx, "abc", "k", GetOptions{})
	testx.RequireNoError(t, err)

	defer obj.Close()
	if _, err := io.ReadAll(obj); err == nil {
		t.Fatal("截断密文读取应报错")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("写入失败")
}
