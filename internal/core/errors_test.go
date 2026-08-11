package core

import (
	"errors"
	testx "github.com/lcylpzls/testx"
	"testing"

	"github.com/lcylpzls/errx"
)

func TestErrorCodesRegistered(t *testing.T) {
	codes := []errx.Code{
		CodeInvalidArgument,
		CodeInvalidConfig,
		CodeInvalidBucket,
		CodeInvalidKey,
		CodeInvalidMetadata,
		CodeInvalidRange,
		CodeInternal,
		CodeUnauthorized,
		CodeForbidden,
		CodeNotModified,
		CodePreconditionFailed,
		CodeBucketExists,
		CodeBucketNotFound,
		CodeBucketNotEmpty,
		CodeObjectNotFound,
		CodeObjectTooLarge,
		CodeChecksumMismatch,
		CodeMetadataCorrupt,
		CodeStorageFailed,
		CodeUploadNotFound,
		CodeUploadInvalid,
		CodeUploadIncomplete,
		CodeVersionNotFound,
		CodeQuotaExceeded,
	}
	for _, code := range codes {
		if errx.Describe(code) == "" {
			t.Errorf("错误码 %s 未注册", code)
		}
		if errx.CodeKind(code) == errx.KindUnknown {
			t.Errorf("错误码 %s 缺少分类", code)
		}
	}
}

func TestErrorConstructors(t *testing.T) {
	err := newCode(CodeObjectNotFound, "对象不存在")
	mustErrCode(t, err, CodeObjectNotFound)
	if errx.KindOf(err) != errx.KindNotFound {
		t.Fatalf("Kind 应为 not_found，实际 %s", errx.KindOf(err))
	}

	errf := newCodef(CodeInvalidBucket, "非法桶名：%s", "A")
	mustErrCode(t, errf, CodeInvalidBucket)

	cause := errors.New("磁盘故障")
	wrapped := wrapCode(cause, CodeStorageFailed, "存储失败")
	mustErrCode(t, wrapped, CodeStorageFailed)
	testx.RequireErrorIs(t, wrapped, cause)

}

func TestErrCodeOf(t *testing.T) {
	err := errx.NewCode(CodeQuotaExceeded, "配额超限")
	if got := errxCode(err); got != string(CodeQuotaExceeded) {
		t.Fatalf("errxCode 应为 %s，实际 %s", CodeQuotaExceeded, got)
	}
	if got := errxCode(nil); got != "" {
		t.Fatalf("nil 错误码应为空，实际 %s", got)
	}
	if got := errxCode(errors.New("普通错误")); got != "unknown" {
		t.Fatalf("普通错误码应为 unknown，实际 %s", got)
	}
}
