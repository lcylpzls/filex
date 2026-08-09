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
    DisableSync    bool          // true 时跳过 fsync，默认 false（即默认 fsync）
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
    Verify bool // 读取时复验 SHA256
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
```

### 2.5 错误

全部错误为 `*errx.Error`，错误码见 [design.md](design.md) 第 7 节；
`errors.Is` / `errx.Is` 均可匹配。

## 3. 协议 API（v0.2.0 定稿）

### 3.1 服务端

```go
type HandlerConfig struct {
    Store   *filex.Store
    Logger  logx.Logger
    Metrics filex.Metrics
    // v0.5.0：Authenticator 鉴权回调
}

func NewHandler(cfg HandlerConfig) http.Handler
```

### 3.2 客户端

```go
type Client struct { ... }
func New(baseURL string, opts ...Option) (*Client, error)
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
