package filex

import (
	"encoding/hex"
	"strings"
)

// validateBucketName 校验桶名：3-63 位，小写字母/数字/连字符，
// 首尾必须是字母或数字。
func validateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return newCodef(CodeInvalidBucket, "桶名长度必须在 3-63 之间，当前 %d", len(name))
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if c == '-' {
			if i == 0 || i == len(name)-1 {
				return newCodef(CodeInvalidBucket, "桶名首尾不能是连字符：%q", name)
			}
			continue
		}
		if !lower && !digit {
			return newCodef(CodeInvalidBucket, "桶名只能包含小写字母、数字与连字符：%q", name)
		}
	}
	return nil
}

// validateKey 校验键名：非空、不超长、不含 NUL，且不是 "." 或 ".."。
func validateKey(key string, maxBytes int) error {
	n := len([]byte(key))
	if key == "" {
		return newCode(CodeInvalidKey, "键名不能为空")
	}
	if n > maxBytes {
		return newCodef(CodeInvalidKey, "键名超过 %d 字节上限，当前 %d", maxBytes, n)
	}
	if key == "." || key == ".." {
		return newCodef(CodeInvalidKey, "键名不能是 %q", key)
	}
	if strings.IndexByte(key, 0) >= 0 {
		return newCode(CodeInvalidKey, "键名不能包含 NUL 字符")
	}
	return nil
}

// validateMetadata 校验自定义元数据条目数量、键与值长度。
func validateMetadata(m map[string]string) error {
	if len(m) > maxMetadataEntries {
		return newCodef(CodeInvalidMetadata, "元数据条目超过 %d 上限", maxMetadataEntries)
	}
	for k, v := range m {
		if k == "" || strings.IndexByte(k, 0) >= 0 {
			return newCode(CodeInvalidMetadata, "元数据键不能为空或包含 NUL")
		}
		if len([]byte(k)) > maxMetadataKeyBytes {
			return newCodef(CodeInvalidMetadata, "元数据键超过 %d 字节上限", maxMetadataKeyBytes)
		}
		if strings.IndexByte(v, 0) >= 0 {
			return newCode(CodeInvalidMetadata, "元数据值不能包含 NUL")
		}
		if len([]byte(v)) > maxMetadataValueBytes {
			return newCodef(CodeInvalidMetadata, "元数据值超过 %d 字节上限", maxMetadataValueBytes)
		}
	}
	return nil
}

// isSHA256Hex 判断字符串是否为 64 位小写/大写十六进制。
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
