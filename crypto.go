package filex

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"

	"github.com/lcylpzls/errx"
)

// cryptoRand 可注入，便于测试随机数失败分支。
var cryptoRand io.Reader = rand.Reader

// encryptionAlgorithm 是静态加密算法：AES-256-CTR + AES-GCM 主密钥包装。
const encryptionAlgorithm = "AES-256-CTR"

// encryptionMeta 是对象元数据中的加密信息。
type encryptionMeta struct {
	Algorithm  string `json:"algorithm"`
	KeyNonce   string `json:"key_nonce"`
	WrappedKey string `json:"wrapped_key"`
	DataNonce  string `json:"data_nonce"`
}

// objectCipher 保存单对象加密上下文。
type objectCipher struct {
	meta      encryptionMeta
	dek       []byte
	dataNonce []byte
}

// newObjectCipher 生成随机 DEK 并用主密钥包装；未配置主密钥时返回 nil。
func newObjectCipher(kek []byte) (*objectCipher, error) {
	if len(kek) == 0 {
		return nil, nil
	}
	dek := make([]byte, 32)
	keyNonce := make([]byte, 12)
	dataNonce := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand, dek); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成数据密钥失败")
	}
	if _, err := io.ReadFull(cryptoRand, keyNonce); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成密钥随机数失败")
	}
	if _, err := io.ReadFull(cryptoRand, dataNonce); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "生成数据随机数失败")
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
			DataNonce:  hex.EncodeToString(dataNonce),
		},
		dek:       dek,
		dataNonce: dataNonce,
	}, nil
}

// newCTRWriter 返回把明文加密写入 dst 的写入器（DEK 由内部保证 32 字节）。
func (c *objectCipher) newCTRWriter(dst io.Writer) io.Writer {
	block, _ := aes.NewCipher(c.dek)
	ctr := cipher.NewCTR(block, c.dataNonce)
	return &ctrWriter{stream: ctr, dst: dst}
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

// ctrWriter 以 CTR 流加密方式写目标。
type ctrWriter struct {
	stream cipher.Stream
	dst    io.Writer
}

func (w *ctrWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	w.stream.XORKeyStream(buf, p)
	return w.dst.Write(buf)
}
