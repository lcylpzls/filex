package proto

import (
	"strconv"
	"strings"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
)

// ParseRange 解析 HTTP Range 头为 filex.ByteRange。
// 第二个返回值表示请求头是否存在；格式非法或越界返回 filex_invalid_range。
func ParseRange(header string, size int64) (filex.ByteRange, bool, error) {
	if header == "" {
		return filex.ByteRange{}, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return filex.ByteRange{}, true, newRangeError("范围必须以 bytes= 开头")
	}
	if size <= 0 {
		return filex.ByteRange{}, true, newRangeError("空对象不支持范围读取")
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if spec == "" {
		return filex.ByteRange{}, true, newRangeError("范围不能为空")
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return filex.ByteRange{}, true, newRangeError("范围缺少连字符")
	}
	startStr, endStr := parts[0], parts[1]
	if startStr == "" {
		// 后缀范围：bytes=-N 表示最后 N 字节
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return filex.ByteRange{}, true, newRangeError("后缀范围必须为正整数")
		}
		if n > size {
			n = size
		}
		return filex.ByteRange{Start: size - n, End: size - 1}, true, nil
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= size {
		return filex.ByteRange{}, true, newRangeError("起始位置非法或越界")
	}
	end := size - 1
	if endStr != "" {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil || end < start {
			return filex.ByteRange{}, true, newRangeError("结束位置非法")
		}
		if end >= size {
			end = size - 1
		}
	}
	return filex.ByteRange{Start: start, End: end}, true, nil
}

func newRangeError(msg string) error {
	return errx.NewCode(filex.CodeInvalidRange, msg)
}
