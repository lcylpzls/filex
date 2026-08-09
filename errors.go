package filex

import "github.com/lcylpzls/errx"

// filex 错误码全集（统一前缀 filex_）。
const (
	CodeInvalidArgument  = errx.Code("filex_invalid_argument")
	CodeInvalidConfig    = errx.Code("filex_invalid_config")
	CodeInvalidBucket    = errx.Code("filex_invalid_bucket")
	CodeInvalidKey       = errx.Code("filex_invalid_key")
	CodeInvalidMetadata  = errx.Code("filex_invalid_metadata")
	CodeInvalidRange     = errx.Code("filex_invalid_range")
	CodeInternal         = errx.Code("filex_internal")
	CodeBucketExists     = errx.Code("filex_bucket_exists")
	CodeBucketNotFound   = errx.Code("filex_bucket_not_found")
	CodeBucketNotEmpty   = errx.Code("filex_bucket_not_empty")
	CodeObjectNotFound   = errx.Code("filex_object_not_found")
	CodeObjectTooLarge   = errx.Code("filex_object_too_large")
	CodeChecksumMismatch = errx.Code("filex_checksum_mismatch")
	CodeMetadataCorrupt  = errx.Code("filex_metadata_corrupt")
	CodeStorageFailed    = errx.Code("filex_storage_failed")
	CodeUploadNotFound   = errx.Code("filex_upload_not_found")
	CodeUploadInvalid    = errx.Code("filex_upload_invalid")
	CodeUploadIncomplete = errx.Code("filex_upload_incomplete")
	CodeVersionNotFound  = errx.Code("filex_version_not_found")
	CodeQuotaExceeded    = errx.Code("filex_quota_exceeded")
)

func init() {
	register(CodeInvalidArgument, "参数非法", errx.KindInvalid)
	register(CodeInvalidConfig, "配置非法", errx.KindInvalid)
	register(CodeInvalidBucket, "桶名非法", errx.KindInvalid)
	register(CodeInvalidKey, "键名非法", errx.KindInvalid)
	register(CodeInvalidMetadata, "元数据非法", errx.KindInvalid)
	register(CodeInvalidRange, "范围非法", errx.KindInvalid)
	register(CodeInternal, "服务器内部错误", errx.KindInternal)
	register(CodeBucketExists, "桶已存在", errx.KindAlreadyExists)
	register(CodeBucketNotFound, "桶不存在", errx.KindNotFound)
	register(CodeBucketNotEmpty, "桶非空", errx.KindConflict)
	register(CodeObjectNotFound, "对象不存在", errx.KindNotFound)
	register(CodeObjectTooLarge, "对象超过大小上限", errx.KindInvalid)
	register(CodeChecksumMismatch, "SHA256 校验失败", errx.KindDataLoss)
	register(CodeMetadataCorrupt, "元数据损坏", errx.KindDataLoss)
	register(CodeStorageFailed, "存储 IO 失败", errx.KindUnavailable)
	register(CodeUploadNotFound, "分片上传会话不存在", errx.KindNotFound)
	register(CodeUploadInvalid, "分片上传参数非法", errx.KindInvalid)
	register(CodeUploadIncomplete, "分片上传不完整", errx.KindInvalid)
	register(CodeVersionNotFound, "对象版本不存在", errx.KindNotFound)
	register(CodeQuotaExceeded, "配额超限", errx.KindQuotaExceeded)
}

func register(code errx.Code, desc string, kind errx.Kind) {
	errx.RegisterCode(code, desc)
	errx.RegisterCodeKind(code, kind)
}

func newCode(code errx.Code, msg string) error {
	return errx.NewCode(code, msg)
}

func newCodef(code errx.Code, format string, args ...any) error {
	return errx.NewCodef(code, format, args...)
}

func wrapCode(err error, code errx.Code, msg string) error {
	return errx.WrapCode(err, code, msg)
}
