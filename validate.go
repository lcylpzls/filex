package filex

import (
	"strconv"
	"strings"

	"github.com/lcylpzls/validx"
)

// init 注册 filex 业务规则到 validx 全局规则表：
// 桶名 / 键名 / 元数据校验统一由 validx 规则体系执行，错误码保持 filex 语义。
func init() {
	_ = validx.RegisterRule("filex_bucket_name", func(value any, param, path string) error {
		// 内部调用保证 value 为 string。
		name := value.(string)
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
	})
	_ = validx.RegisterRule("filex_key", func(value any, param, path string) error {
		// 内部调用保证 value 为 string、param 为合法整数。
		key := value.(string)
		maxBytes, _ := strconv.Atoi(param)
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
	})
	_ = validx.RegisterRule("filex_metadata", func(value any, param, path string) error {
		// 内部调用保证 value 为 map[string]string。
		m := value.(map[string]string)
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
	})
}

// validateBucketName 校验桶名：3-63 位，小写字母/数字/连字符，
// 首尾必须是字母或数字。
func validateBucketName(name string) error {
	return validx.ValidateField(name, "filex_bucket_name")
}

// validateKey 校验键名：非空、不超长、不含 NUL，且不是 "." 或 ".."。
func validateKey(key string, maxBytes int) error {
	return validx.ValidateField(key, "filex_key="+strconv.Itoa(maxBytes))
}

// validateMetadata 校验自定义元数据条目数量、键与值长度。
func validateMetadata(m map[string]string) error {
	return validx.ValidateField(m, "filex_metadata")
}

// isSHA256Hex 判断字符串是否为 64 位小写/大写十六进制。
func isSHA256Hex(s string) bool {
	return validx.ValidateField(s, "hexadecimal,len=64") == nil
}
