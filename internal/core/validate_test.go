package core

import (
	"github.com/lcylpzls/testx"
	"strconv"
	"strings"
	"testing"
)

func TestValidateBucketName(t *testing.T) {
	valid := []string{"abc", "a-b-c", "a1b2", "aaa"}
	for _, name := range valid {
		if err := validateBucketName(name); err != nil {
			t.Errorf("桶名 %q 应合法：%v", name, err)
		}
	}
	invalid := []string{"ab", "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz12345", "ABC", "a_b", "-ab", "ab-", "a..b", "a b"}
	for _, name := range invalid {
		if err := validateBucketName(name); err == nil {
			t.Errorf("桶名 %q 应非法", name)
		} else {
			mustErrCode(t, err, CodeInvalidBucket)
		}
	}
}

func TestValidateKey(t *testing.T) {
	valid := []string{"a", "dir/file.txt", "中文键", "a b", strings.Repeat("k", defaultMaxKeyBytes)}
	for _, key := range valid {
		if err := validateKey(key, defaultMaxKeyBytes); err != nil {
			t.Errorf("键 %q 应合法：%v", key, err)
		}
	}
	invalid := []struct {
		key string
	}{
		{""},
		{"."},
		{".."},
		{string([]byte{'a', 0, 'b'})},
		{strings.Repeat("k", defaultMaxKeyBytes+1)},
	}
	for _, c := range invalid {
		if err := validateKey(c.key, defaultMaxKeyBytes); err == nil {
			t.Errorf("键 %q 应非法", c.key)
		} else {
			mustErrCode(t, err, CodeInvalidKey)
		}
	}
}

func TestValidateMetadata(t *testing.T) {
	testx.RequireNoError(t, validateMetadata(map[string]string{"k": "v"}))
	testx.RequireNoError(t, validateMetadata(nil))
	tooMany := map[string]string{}
	for i := 0; i <= maxMetadataEntries; i++ {
		tooMany[string(rune('a'+i%26))+strconv.Itoa(i)] = "v"
	}
	mustErrCode(t, validateMetadata(tooMany), CodeInvalidMetadata)
	mustErrCode(t, validateMetadata(map[string]string{"": "v"}), CodeInvalidMetadata)
	mustErrCode(t, validateMetadata(map[string]string{"a\x00b": "v"}), CodeInvalidMetadata)
	mustErrCode(t, validateMetadata(map[string]string{strings.Repeat("k", maxMetadataKeyBytes+1): "v"}), CodeInvalidMetadata)
	mustErrCode(t, validateMetadata(map[string]string{"k": "v\x00x"}), CodeInvalidMetadata)
	mustErrCode(t, validateMetadata(map[string]string{"k": strings.Repeat("v", maxMetadataValueBytes+1)}), CodeInvalidMetadata)
}

func TestIsSHA256Hex(t *testing.T) {
	ok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !isSHA256Hex(ok) {
		t.Fatal("64 位十六进制应通过")
	}
	if isSHA256Hex(ok[:63]) {
		t.Fatal("长度不足应拒绝")
	}
	if isSHA256Hex("zz" + ok[2:]) {
		t.Fatal("非十六进制字符应拒绝")
	}
}
