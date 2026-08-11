package core

import (
	"strings"
	"testing"
)

func FuzzValidateBucket(f *testing.F) {
	for _, seed := range []string{"abc", "BAD", "ab", "-ab", "a-b", strings.Repeat("a", 64)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		_ = validateBucketName(name)
	})
}

func FuzzValidateKey(f *testing.F) {
	for _, seed := range []string{"a", "", ".", "..", strings.Repeat("k", 2048), "a\x00b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		_ = validateKey(key, defaultMaxKeyBytes)
	})
}

func FuzzDecodeObjectMeta(f *testing.F) {
	good := `{"key":"k","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}`
	for _, seed := range []string{good, `{`, `{"key":"k","size":-1}`, `{"key":"k","size":1,"sha256":"bad"}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeObjectMeta(data)
	})
}

func FuzzDecodeUploadMeta(f *testing.F) {
	for _, seed := range []string{
		`{"upload_id":"u1","bucket":"abc","key":"k"}`,
		`{`,
		`{"upload_id":"u1"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeUploadMeta(data)
	})
}

func FuzzDecodePartMeta(f *testing.F) {
	for _, seed := range []string{
		`{"part_number":1,"size":1,"sha256":"` + strings.Repeat("a", 64) + `"}`,
		`{`,
		`{"part_number":0}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodePartMeta(data)
	})
}

func FuzzDecodeBucketMeta(f *testing.F) {
	for _, seed := range []string{
		`{"name":"abc","created_at":"2026-08-10T00:00:00Z"}`,
		`{`,
		`{"name":""}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeBucketMeta(data)
	})
}
