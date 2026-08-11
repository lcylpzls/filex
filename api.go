package filex

import "github.com/lcylpzls/filex/internal/core"

type (
	ObjectEvent      = core.ObjectEvent
	EventHook        = core.EventHook
	BucketStats      = core.BucketStats
	LifecycleOptions = core.LifecycleOptions
	LifecycleReport  = core.LifecycleReport
	SweepReport      = core.SweepReport
	IntegrityReport  = core.IntegrityReport
	Config           = core.Config
	Logger           = core.Logger
	Metrics          = core.Metrics
	TraceAttr        = core.TraceAttr
	TraceHook        = core.TraceHook
	BucketInfo       = core.BucketInfo
	ObjectInfo       = core.ObjectInfo
	Object           = core.Object
	PutOptions       = core.PutOptions
	GetOptions       = core.GetOptions
	ByteRange        = core.ByteRange
	ListOptions      = core.ListOptions
	ListResult       = core.ListResult
	UploadInfo       = core.UploadInfo
	PartInfo         = core.PartInfo
	Store            = core.Store
)

const (
	CodeInvalidArgument    = core.CodeInvalidArgument
	CodeInvalidConfig      = core.CodeInvalidConfig
	CodeInvalidBucket      = core.CodeInvalidBucket
	CodeInvalidKey         = core.CodeInvalidKey
	CodeInvalidMetadata    = core.CodeInvalidMetadata
	CodeInvalidRange       = core.CodeInvalidRange
	CodeInternal           = core.CodeInternal
	CodeUnauthorized       = core.CodeUnauthorized
	CodeForbidden          = core.CodeForbidden
	CodeNotModified        = core.CodeNotModified
	CodePreconditionFailed = core.CodePreconditionFailed
	CodeCancelled          = core.CodeCancelled
	CodeClosed             = core.CodeClosed
	CodeBucketExists       = core.CodeBucketExists
	CodeBucketNotFound     = core.CodeBucketNotFound
	CodeBucketNotEmpty     = core.CodeBucketNotEmpty
	CodeObjectNotFound     = core.CodeObjectNotFound
	CodeObjectTooLarge     = core.CodeObjectTooLarge
	CodeChecksumMismatch   = core.CodeChecksumMismatch
	CodeMetadataCorrupt    = core.CodeMetadataCorrupt
	CodeStorageFailed      = core.CodeStorageFailed
	CodeUploadNotFound     = core.CodeUploadNotFound
	CodeUploadInvalid      = core.CodeUploadInvalid
	CodeUploadIncomplete   = core.CodeUploadIncomplete
	CodeVersionNotFound    = core.CodeVersionNotFound
	CodeQuotaExceeded      = core.CodeQuotaExceeded
)

func New(cfg Config) (*Store, error) { return core.New(cfg) }
