package filex

import (
	"io"
	"time"

	"github.com/lcylpzls/logx"
	"github.com/lcylpzls/metricsx"
)

// 默认与上限常量。
const (
	defaultMaxObjectSize  = int64(4) << 30 // 默认单对象上限 4 GiB
	defaultMaxKeyBytes    = 1024           // 默认键最大 1024 字节
	defaultListLimit      = 1000           // 默认列表分页大小
	maxListLimit          = 10000          // 单次列表上限
	maxMetadataEntries    = 64             // 自定义元数据条目上限
	maxMetadataKeyBytes   = 128            // 元数据键最大字节数
	maxMetadataValueBytes = 4096           // 元数据值最大字节数
)

// Config 是 Store 的配置。
type Config struct {
	// DataDir 是数据根目录（必填）。
	DataDir string
	// MaxObjectSize 是单对象大小上限；0 表示默认 4 GiB。
	MaxObjectSize int64
	// MaxKeyBytes 是键最大字节数；0 表示默认 1024。
	MaxKeyBytes int
	// MaxParts 是单次分片上传的部件数量上限；0 表示默认 10000。
	MaxParts int
	// UploadTTL 是分片会话保留时长；0 表示默认 24 小时。
	UploadTTL time.Duration
	// DisableSync 为 true 时跳过 fsync（默认 false，即默认落盘强一致）。
	DisableSync bool
	// EncryptionKey 是 32 字节主密钥；设置后启用服务端静态加密。
	EncryptionKey []byte
	// Logger 是可选结构化日志。
	Logger Logger
	// Metrics 是可选指标打点。
	Metrics Metrics
	// TraceHook 是可选链路追踪钩子。
	TraceHook TraceHook
	// EventHook 是可选事件钩子（默认 no-op），由 eventx 等外部适配器接入。
	EventHook EventHook
}

// Logger 是 filex 使用的最小日志接口，logx.Logger 天然满足。
type Logger interface {
	Info(msg string, fields logx.FieldGroup)
	Warn(msg string, fields logx.FieldGroup)
	Error(msg string, fields logx.FieldGroup)
}

// Metrics 是 filex 的指标打点接口，可对接 metricsx。
// Metrics 是最小指标协议（家族统一契约，定义见 metricsx.Sink）。
type Metrics = metricsx.Sink

// BucketInfo 是桶元数据快照。
type BucketInfo struct {
	Name       string
	Versioning bool
	Quota      int64 // 0 表示不限
	Lifecycle  LifecycleOptions
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ObjectInfo 是对象元数据快照。
type ObjectInfo struct {
	Bucket      string
	Key         string
	Size        int64
	ETag        string // 内容 SHA256 十六进制
	ContentType string
	Metadata    map[string]string
	VersionID   string
	Deleted     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Object 是对象读取句柄：携带元数据快照与流式内容。
type Object struct {
	Info ObjectInfo
	io.ReadCloser
}

// PutOptions 是 Put 的选项。
type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	// ExpectedSHA256 提供时写入前强制校验内容哈希（64 位十六进制）。
	ExpectedSHA256 string
}

// GetOptions 是 Get 的选项。
type GetOptions struct {
	// Verify 为 true 时读取过程中流式复验 SHA256，EOF 处校验。
	Verify bool
	// Range 请求指定字节范围（含端点）；与 Verify 互斥。
	Range *ByteRange
	// IfMatch 提供时仅当 ETag 匹配才返回内容（协议层使用）。
	IfMatch string
	// IfNoneMatch 提供时仅当 ETag 不匹配才返回内容（协议层使用）。
	IfNoneMatch string
}

// ByteRange 表示对象内容的字节范围（含端点）。
type ByteRange struct {
	Start int64
	End   int64
}

// Length 返回范围长度。
func (r ByteRange) Length() int64 {
	return r.End - r.Start + 1
}

// ListOptions 是 List 的选项。
type ListOptions struct {
	Prefix    string
	Marker    string // 排除字典序小于等于 marker 的键
	Limit     int    // 0 表示默认 1000，最大 10000
	Delimiter string // 通常为 "/"，用于聚合公共前缀
}

// ListResult 是 List 的结果。
type ListResult struct {
	Objects        []ObjectInfo
	CommonPrefixes []string
	NextMarker     string
	IsTruncated    bool
}
