package core

import (
	"io"

	"github.com/lcylpzls/cryptox"
)

// encryptionAlgorithm 是静态加密算法标识：cryptox 分块 AES-256-GCM 流。
const encryptionAlgorithm = "CRYPTOX-AES-256-GCM"

// encryptionMeta 是对象元数据中的加密信息。
// v0.25.0 起加密数据为 cryptox 自包含流，元数据仅记录算法标识。
type encryptionMeta struct {
	Algorithm string `json:"algorithm"`
}

// objectCipher 保存单对象加密上下文。
type objectCipher struct {
	enabled bool
}

// newObjectCipher 返回加密标记；未配置主密钥时返回 nil。
// 主密钥长度由 Store 构造时校验（16/24/32 字节）。
func newObjectCipher(kek []byte) *objectCipher {
	if len(kek) == 0 {
		return nil
	}
	return &objectCipher{enabled: true}
}

// encryptPipe 返回明文写入端与结束函数：
// goroutine 将写入的明文经 cryptox.EncryptStream 加密到 dst。
// 结束函数关闭写入端并等待加密完成。
func encryptPipe(kek []byte, dst io.Writer) (io.Writer, func() error) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := cryptox.EncryptStream(kek, dst, pr)
		// 写入端必须关闭：加密失败时让写入方得到错误而非阻塞。
		_ = pw.CloseWithError(err)
		done <- err
	}()
	return pw, func() error {
		_ = pw.Close()
		return <-done
	}
}

// decryptReader 将 cryptox 流解密为可读流；解密错误经读取传播，
// 并归一为对象认证失败错误码。
func decryptReader(kek []byte, src io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		err := cryptox.DecryptStream(kek, pw, src)
		if err != nil {
			err = wrapCode(err, CodeChecksumMismatch, "对象密文认证失败")
		}
		_ = pw.CloseWithError(err)
	}()
	return pr
}
