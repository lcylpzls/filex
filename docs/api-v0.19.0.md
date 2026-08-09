# filex API 定版

> 本文档随版本演进；每个发布版本会冻结对应 API 快照。

## 1. 包结构

| 包 | 职责 | 起始版本 |
| --- | --- | --- |
| `filex` | 存储引擎：Store / Bucket / ObjectInfo / Options / Errors | v0.1.0 |
| `filex/server` | 自研协议 HTTP 处理器 | v0.2.0 |
| `filex/client` | 自研协议客户端 | v0.2.0 |
| `filex/proto` | 协议消息与常量（两包共享） | v0.2.0 |

## 2. 引擎 API（v0.1.0）

### 2.1 Config 与 Store

```go
type Config struct {
    DataDir        string        // 数据根目录（必填）
    MaxObjectSize  int64         // 对象大小上限，默认 4 GiB
    MaxKeyBytes    int           // 键最大字节数，默认 1024
    MaxParts       int           // 分片部件数量上限，默认 10000
    DisableSync    bool          // true 时跳过 fsync，默认 false（即默认 fsync）
    EncryptionKey  []byte        // 32 字节主密钥；设置后启用静态加密
    UploadTTL      time.Duration // 分片会话保留时长，默认 24 小时
    Logger         logx.Logger   // 可选结构化日志
    Metrics        Metrics       // 可选指标
}

func New(cfg Config) (*Store, error)
func (s *Store) Close() error
```

### 2.2 Bucket

```go
func (s *Store) CreateBucket(ctx context.Context, name string) (BucketInfo, error)
func (s *Store) DeleteBucket(ctx context.Context, name string) error // 仅空桶
func (s *Store) ListBuckets(ctx context.Context) ([]BucketInfo, error)
func (s *Store) HeadBucket(ctx context.Context, name string) (BucketInfo, error)
```

### 2.3 Object

```go
type PutOptions struct {
    ContentType   string
    Metadata      map[string]string
    ExpectedSHA256 string // 可选：写入前校验
}

type GetOptions struct {
    Verify      bool        // 读取时复验 SHA256
    Range       *ByteRange  // 字节范围读取（与 Verify 互斥）
    IfMatch     string      // 仅 ETag 匹配时返回（协议层）
    IfNoneMatch string      // 仅 ETag 不匹配时返回（协议层）
}

type ListOptions struct {
    Prefix    string
    Marker    string
    Limit     int    // 0 表示默认 1000
    Delimiter string // 通常 "/"
}

func (s *Store) Put(ctx context.Context, bucket, key string,
    r io.Reader, opts PutOptions) (ObjectInfo, error)
func (s *Store) Get(ctx context.Context, bucket, key string,
    opts GetOptions) (*Object, error)          // 返回流式读取器
func (s *Store) Head(ctx context.Context, bucket, key string) (ObjectInfo, error)
func (s *Store) Delete(ctx context.Context, bucket, key string) error
func (s *Store) List(ctx context.Context, bucket string,
    opts ListOptions) (ListResult, error)
```

### 2.4 类型

```go
type ObjectInfo struct {
    Bucket      string
    Key         string
    Size        int64
    ETag        string // 内容 SHA256 十六进制
    ContentType string
    Metadata    map[string]string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Object struct {
    Info ObjectInfo
    io.ReadCloser
}

type ListResult struct {
    Objects        []ObjectInfo
    CommonPrefixes []string
    NextMarker     string
    IsTruncated    bool
}

type ByteRange struct {
    Start int64 // 起始字节（含）
    End   int64 // 结束字节（含）
}
```

### 2.5 错误

全部错误为 `*errx.Error`，错误码见 [design.md](design.md) 第 7 节；
`errors.Is` / `errx.Is` 均可匹配。

## 3. 协议 API（v0.2.0 定稿）

### 3.1 服务端

```go
type HandlerConfig struct {
    Store         Store                       // *filex.Store 天然满足
    Logger        logx.Logger                 // 可选结构化日志
    Token         string                  // Bearer 令牌（可选）
    Authenticate  func(r *http.Request) error // 鉴权回调（可选）
    HMACSecret    []byte                  // HMAC 签名密钥（可选）
    Audit         func(AuditEvent)        // 审计回调（可选）
}

func NewHandler(cfg HandlerConfig) http.Handler
```

`HandlerConfig.Store` 为接口，`*filex.Store` 可直接使用。

### 3.2 客户端

```go
type Client struct { ... }
func New(baseURL string, opts ...Option) (*Client, error)
// Option：WithHTTPClient / WithToken / WithHMAC
func (c *Client) Put(ctx context.Context, bucket, key string,
    r io.Reader, opts filex.PutOptions) (filex.ObjectInfo, error)
func (c *Client) Get(ctx context.Context, bucket, key string,
    opts filex.GetOptions) (*filex.Object, error)
func (c *Client) Head(...) (filex.ObjectInfo, error)
func (c *Client) Delete(...) error
func (c *Client) List(...) (filex.ListResult, error)
```

## 4. 后续版本

- v0.3.0：`Multipart`（Initiate / UploadPart / Complete / Abort）API；
- v0.4.0：`VersionID`、`ListVersions`、`Restore`、`Copy`、`Move`、配额；
- v0.5.0：`Encryption`（AES-GCM）、`Authenticator`、`AuditLog`；
- v0.6.0：`Lifecycle`（过期清理）。

具体签名在对应版本设计定稿后冻结。

## 4.1 分片上传 API（v0.3.0）

```go
func (s *Store) InitiateMultipartUpload(ctx context.Context, bucket, key string,
    opts PutOptions) (UploadInfo, error)
func (s *Store) UploadPart(ctx context.Context, bucket, key, uploadID string,
    partNumber int, r io.Reader) (PartInfo, error)
func (s *Store) CompleteMultipartUpload(ctx context.Context, bucket, key,
    uploadID string) (ObjectInfo, error)
func (s *Store) AbortMultipartUpload(ctx context.Context, bucket, key,
    uploadID string) error
func (s *Store) ListParts(ctx context.Context, bucket, key,
    uploadID string) ([]PartInfo, error)
```

客户端在 `client.Client` 上提供同名方法，并新增：

```go
func (c *Client) PutMultipart(ctx context.Context, bucket, key string,
    r io.Reader, opts filex.PutOptions, partSize int64,
    concurrency int) (filex.ObjectInfo, error)
```

`PutMultipart` 默认部件 16 MiB、并发 4；失败自动 Abort。

协议端点（PUT 承载）：

```text
PUT    /filex/v1/buckets/{bucket}/objects/{key}?upload=initiate
PUT    /filex/v1/buckets/{bucket}/objects/{key}?upload=part&upload-id=..&part-number=..
PUT    /filex/v1/buckets/{bucket}/objects/{key}?upload=complete&upload-id=..
GET    /filex/v1/buckets/{bucket}/objects/{key}?upload=parts&upload-id=..
DELETE /filex/v1/buckets/{bucket}/objects/{key}?upload=abort&upload-id=..
```

## 4.2 版本化与对象管理 API（v0.4.0）

```go
func (s *Store) SetBucketVersioning(ctx context.Context, name string,
    enabled bool) (BucketInfo, error)
func (s *Store) SetBucketQuota(ctx context.Context, name string,
    quota int64) (BucketInfo, error)
func (s *Store) GetVersion(ctx context.Context, bucket, key, versionID string,
    opts GetOptions) (*Object, error)
func (s *Store) HeadVersion(ctx context.Context, bucket, key,
    versionID string) (ObjectInfo, error)
func (s *Store) DeleteVersion(ctx context.Context, bucket, key,
    versionID string) error
func (s *Store) RestoreVersion(ctx context.Context, bucket, key,
    versionID string) (ObjectInfo, error)
func (s *Store) ListVersions(ctx context.Context, bucket, key string) ([]ObjectInfo, error)
func (s *Store) Copy(ctx context.Context, srcBucket, srcKey, dstBucket,
    dstKey string) (ObjectInfo, error)
func (s *Store) Move(ctx context.Context, srcBucket, srcKey, dstBucket,
    dstKey string) (ObjectInfo, error)
```

协议端点：

```text
PUT    /filex/v1/buckets/{bucket}?versioning=true|false
PUT    /filex/v1/buckets/{bucket}?quota=N
GET    /filex/v1/buckets/{bucket}/objects/{key}?version-id=..
GET    /filex/v1/buckets/{bucket}/objects/{key}?versions=true
HEAD   /filex/v1/buckets/{bucket}/objects/{key}?version-id=..
DELETE /filex/v1/buckets/{bucket}/objects/{key}?version-id=..
PUT    /filex/v1/buckets/{bucket}/objects/{key}?restore=1&version-id=..
PUT    /filex/v1/buckets/{bucket}/objects/{key}?copy=1&source-bucket=..&source-key=..
PUT    /filex/v1/buckets/{bucket}/objects/{key}?move=1&source-bucket=..&source-key=..
```

鉴权约定（v0.5.0）：

- Bearer：`Authorization: Bearer <Token>`；
- HMAC：`X-Filex-Timestamp`（Unix 秒）+ `X-Filex-Signature`；
- 签名载荷：`<METHOD>\n<路径与查询>\n<时间戳>`，HMAC-SHA256 十六进制；
- 时间戳窗口 ±5 分钟，防重放。
- 条件请求：`If-None-Match` 命中返回 `filex_not_modified`（304）；
  `If-Match` 不匹配返回 `filex_precondition_failed`（412）。

## 4.3 生命周期与可观测 API（v0.6.0）

```go
func (s *Store) SetBucketLifecycle(ctx context.Context, bucket string,
    opts LifecycleOptions) (BucketInfo, error)
func (s *Store) RunLifecycle(ctx context.Context, bucket string) (LifecycleReport, error)
func (s *Store) SweepOrphans(ctx context.Context) (SweepReport, error)
func (s *Store) Health(ctx context.Context) error
func (c *Client) Health(ctx context.Context) error
func (c *Client) GetBucket(ctx context.Context, name string) (BucketInfo, error)
func (s *Store) VerifyObject(ctx context.Context, bucket, key string) error
func (s *Store) VerifyAll(ctx context.Context, concurrency int) (IntegrityReport, error)
func (s *Store) BucketUsage(ctx context.Context, bucket string) (int64, error)
func (s *Store) BucketStats(ctx context.Context, bucket string) (BucketStats, error)
```

协议端点：`GET /filex/v1/health` 返回 `{"status":"ok"}`。

`HandlerConfig.Metrics` 注入后按请求状态统计 `http_request` 与错误码。

`Store.Close` 后所有操作返回 `filex_closed`；重复关闭幂等。

## 5. 运维

见 [operations.md](operations.md)：备份恢复、日常巡检、健康检查、
安全基线与升级流程。
