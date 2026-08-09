package filex

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"

	"github.com/lcylpzls/errx"
)

// encryptionAlgorithm 是静态加密算法：分块 AES-256-GCM（64 KiB/块）。
const encryptionAlgorithm = "AES-256-GCM-CHUNKED"

// encryptionChunkSize 是单块明文大小。
const encryptionChunkSize = 64 * 1024

// encryptionFileNonceSize 是文件级随机数大小。
const encryptionFileNonceSize = 8

// encryptionMeta 是对象元数据中的加密信息。
type encryptionMeta struct {
	Algorithm  string `json:"algorithm"`
	KeyNonce   string `json:"key_nonce"`
	WrappedKey string `json:"wrapped_key"`
	FileNonce  string `json:"file_nonce"`
}

// objectCipher 保存单对象加密上下文。
type objectCipher struct {
	meta      encryptionMeta
	dek       []byte
	fileNonce []byte
}

// cryptoRand 可注入，便于测试随机数失败分支。
var cryptoRand io.Reader = rand.Reader

// newObjectCipher 生成随机 DEK 并用主密钥包装；未配置主密钥时返回 nil。
func newObjectCipher(kek []byte) (*objectCipher, error) {
	if len(kek) == 0 {
		return nil, nil
	}
	dek := make([]byte, 32)
	keyNonce := make([]byte, 12)
	fileNonce := make([]byte, encryptionFileNonceSize)
	if _, err := io.ReadFull(cryptoRand, dek); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成数据密钥失败")
	}
	if _, err := io.ReadFull(cryptoRand, keyNonce); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成密钥随机数失败")
	}
	if _, err := io.ReadFull(cryptoRand, fileNonce); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成文件随机数失败")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, errx.NewCode(CodeInvalidConfig, "加密主密钥必须是 16/24/32 字节")
	}
	gcm, _ := cipher.NewGCM(block)
	wrapped := gcm.Seal(nil, keyNonce, dek, nil)
	return &objectCipher{
		meta: encryptionMeta{
			Algorithm:  encryptionAlgorithm,
			KeyNonce:   hex.EncodeToString(keyNonce),
			WrappedKey: base64.StdEncoding.EncodeToString(wrapped),
			FileNonce:  hex.EncodeToString(fileNonce),
		},
		dek:       dek,
		fileNonce: fileNonce,
	}, nil
}

// newGCMWriter 返回分块认证加密写入器（DEK 由内部保证 32 字节）。
func (c *objectCipher) newGCMWriter(dst io.Writer) *gcmChunkWriter {
	block, _ := aes.NewCipher(c.dek)
	gcm, _ := cipher.NewGCM(block)
	return &gcmChunkWriter{
		gcm:       gcm,
		fileNonce: append([]byte(nil), c.fileNonce...),
		dst:       dst,
		buf:       make([]byte, 0, encryptionChunkSize),
	}
}

// unwrapObjectKey 用主密钥解包对象 DEK。
func unwrapObjectKey(kek []byte, m encryptionMeta) ([]byte, error) {
	if len(kek) == 0 {
		return nil, errx.NewCode(CodeStorageFailed, "缺少加密主密钥")
	}
	wrapped, err := base64.StdEncoding.DecodeString(m.WrappedKey)
	if err != nil {
		return nil, errx.NewCode(CodeMetadataCorrupt, "包装密钥格式非法")
	}
	keyNonce, err := hex.DecodeString(m.KeyNonce)
	if err != nil {
		return nil, errx.NewCode(CodeMetadataCorrupt, "密钥随机数格式非法")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, errx.NewCode(CodeInvalidConfig, "加密主密钥非法")
	}
	gcm, _ := cipher.NewGCM(block)
	dek, err := gcm.Open(nil, keyNonce, wrapped, nil)
	if err != nil {
		return nil, errx.NewCode(CodeMetadataCorrupt, "对象密钥解包失败")
	}
	return dek, nil
}

// gcmChunkWriter 按 64 KiB 分块执行 AES-GCM 认证加密。
type gcmChunkWriter struct {
	gcm       cipher.AEAD
	fileNonce []byte
	dst       io.Writer
	buf       []byte
	idx       uint32
}

func (w *gcmChunkWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if len(w.buf) == encryptionChunkSize {
			if err := w.flush(); err != nil {
				return total, err
			}
		}
		n := copy(w.buf[len(w.buf):encryptionChunkSize], p)
		w.buf = w.buf[:len(w.buf)+n]
		p = p[n:]
		total += n
	}
	return total, nil
}

func (w *gcmChunkWriter) flush() error {
	nonce := make([]byte, 12)
	copy(nonce, w.fileNonce)
	binary.BigEndian.PutUint32(nonce[8:], w.idx)
	w.idx++
	sealed := w.gcm.Seal(nil, nonce, w.buf, nil)
	if _, err := w.dst.Write(sealed); err != nil {
		return err
	}
	w.buf = w.buf[:0]
	return nil
}

// Close 冲刷最后一块（空对象也会写入认证标签）。
func (w *gcmChunkWriter) Close() error {
	return w.flush()
}

// gcmChunkReader 分块解密并认证对象内容。
type gcmChunkReader struct {
	f         io.Reader
	gcm       cipher.AEAD
	fileNonce []byte
	idx       uint32
	remaining int64
	buf       []byte
	pos       int
	err       error
}

func newGCMChunkReader(f io.Reader, gcm cipher.AEAD, fileNonce []byte, size int64) *gcmChunkReader {
	return &gcmChunkReader{
		f:         f,
		gcm:       gcm,
		fileNonce: fileNonce,
		remaining: size,
	}
}

func (r *gcmChunkReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.pos >= len(r.buf) {
		if r.remaining <= 0 {
			r.err = io.EOF
			return 0, io.EOF
		}
		clen := int64(encryptionChunkSize)
		if clen > r.remaining {
			clen = r.remaining
		}
		sealed := make([]byte, clen+16)
		if _, err := io.ReadFull(r.f, sealed); err != nil {
			r.err = err
			return 0, err
		}
		nonce := make([]byte, 12)
		copy(nonce, r.fileNonce)
		binary.BigEndian.PutUint32(nonce[8:], r.idx)
		r.idx++
		plain, err := r.gcm.Open(nil, nonce, sealed, nil)
		if err != nil {
			r.err = errx.NewCode(CodeChecksumMismatch, "对象密文认证失败")
			return 0, r.err
		}
		r.buf = plain
		r.pos = 0
		r.remaining -= int64(len(plain))
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}
