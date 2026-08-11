package filex_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lcylpzls/filex"
)

// TestPublicAPI 黑盒冒烟测试：覆盖根包转发函数、类型别名与错误码常量。
func TestPublicAPI(t *testing.T) {
	s, err := filex.New(filex.Config{DataDir: t.TempDir()})
	if err != nil || s == nil {
		t.Fatalf("New 失败：%v", err)
	}
	defer s.Close()

	if _, err := s.CreateBucket(context.Background(), "my-bucket"); err != nil {
		t.Fatalf("CreateBucket 失败：%v", err)
	}
	if _, err := s.Put(context.Background(), "my-bucket", "k", strings.NewReader("数据"), filex.PutOptions{}); err != nil {
		t.Fatalf("Put 失败：%v", err)
	}
	obj, err := s.Get(context.Background(), "my-bucket", "k", filex.GetOptions{})
	if err != nil || obj == nil {
		t.Fatalf("Get 失败：%v", err)
	}
	_ = obj.Close()
	if err := s.Delete(context.Background(), "my-bucket", "k"); err != nil {
		t.Fatalf("Delete 失败：%v", err)
	}
	_, _ = s.ListBuckets(context.Background())

	var _ filex.ObjectEvent
	var _ filex.EventHook
	var _ filex.BucketStats
	var _ filex.LifecycleOptions
	var _ filex.LifecycleReport
	var _ filex.SweepReport
	var _ filex.IntegrityReport
	var _ filex.Logger
	var _ filex.Metrics
	var _ filex.TraceAttr
	var _ filex.TraceHook
	var _ filex.BucketInfo
	var _ filex.ObjectInfo
	var _ filex.Object
	var _ filex.PutOptions
	var _ filex.GetOptions
	var _ filex.ByteRange
	var _ filex.ListOptions
	var _ filex.ListResult
	var _ filex.UploadInfo
	var _ filex.PartInfo
	_ = filex.CodeInvalidArgument
	_ = filex.CodeInvalidConfig
	_ = filex.CodeInvalidBucket
	_ = filex.CodeInvalidKey
	_ = filex.CodeInvalidMetadata
	_ = filex.CodeInvalidRange
	_ = filex.CodeInternal
	_ = filex.CodeUnauthorized
	_ = filex.CodeForbidden
	_ = filex.CodeNotModified
	_ = filex.CodePreconditionFailed
	_ = filex.CodeCancelled
	_ = filex.CodeClosed
	_ = filex.CodeBucketExists
	_ = filex.CodeBucketNotFound
	_ = filex.CodeBucketNotEmpty
	_ = filex.CodeObjectNotFound
	_ = filex.CodeObjectTooLarge
	_ = filex.CodeChecksumMismatch
	_ = filex.CodeMetadataCorrupt
	_ = filex.CodeStorageFailed
	_ = filex.CodeUploadNotFound
	_ = filex.CodeUploadInvalid
	_ = filex.CodeUploadIncomplete
}
