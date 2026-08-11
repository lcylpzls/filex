# filex

自用对象存储 Go 库：本地盘后端 + 自研传输协议（HTTP/1.1、HTTP/2、HTTP/3），
与 errx / logx / metricsx / webx / httpx 生态原生打通。

> 定位声明：**不兼容 S3 / OSS / WebDAV 等第三方协议**。filex 是自用组件，
> 协议从零设计，不为历史兼容付成本。

## 设计目标

- 轻：核心零第三方依赖（仅标准库 + errx / logx），单 Go 模块即用；
- 稳：原子写入、SHA256 完整性、并发安全、三平台 CI、fuzz、race；
- 快：流式读写、分片上传、断点续传、范围读取；
- 透：errx 错误码、logx 结构化日志、metrics 打点、协议自描述；
- 可控：存储布局简单（普通目录 + JSON 元数据），可备份、可迁移、可审计。

## 快速上手（v0.1.0 核心引擎）

```go
import (
	"context"
	"strings"

	"github.com/lcylpzls/filex"
)

func main() {
	ctx := context.Background()
	store, err := filex.New(filex.Config{DataDir: "./data"})
	if err != nil {
		panic(err)
	}

	_ = store.CreateBucket(ctx, "my-bucket")
	info, err := store.Put(ctx, "my-bucket", "notes/hello.txt",
		strings.NewReader("你好，filex"), filex.PutOptions{ContentType: "text/plain; charset=utf-8"})
	if err != nil {
		panic(err)
	}
	_ = info
}
```

## 已发布版本

- v0.1.0：核心引擎（Bucket/Object 生命周期、原子写、SHA256 完整性、
  元数据、并发安全、错误码与日志）。
- v0.2.0：自研协议服务端与客户端（流式 Put/Get、Range、条件请求、
  分页列表、统一错误 JSON、请求 ID）。
- v0.3.0：分片上传与断点续传（部件级 SHA256、乱序/覆盖、并发上传、
  失败自动中止）。
- v0.4.0：版本化与软删除、Copy/Move、桶配额（超限自动回滚）。
- v0.5.0：服务端静态加密（AES-256-CTR，v0.17 升级为 GCM）、
  Bearer/HMAC/回调鉴权、防重放与审计。
- v0.6.0：生命周期（过期清理/版本收敛）、孤儿巡检、健康检查、
  服务端请求指标与基准。
- v0.7.0：HTTP/3 端到端示例（webx + httpx）、错误码手册与 API 冻结快照；
  路线图完成，进入自检打磨阶段。
- v0.8.0：自检修复（孤儿巡检保护活动分片会话）、架构文档刷新。
- v0.9.0：条件请求错误码明确化（304/412 语义映射）。
- v0.10.0：版本 ID 响应头、1.0 候选终审与 API 冻结快照。
- v0.11.0：数据完整性审计（VerifyObject/VerifyAll）、公开桶用量、
  上下文取消语义。
- v0.12.0：分片会话 TTL 清理、DeleteBucket 活动上传保护、CI 全平台
  race + 乱序测试、仓库工业化。
- v0.13.0：客户端自动 Content-Length、并发基准。
- v0.14.0：BucketStats 统计、运维手册、版本化迁移兼容修复。
- v0.15.0：桶元数据 fuzz、API 冻结快照更新与终审同步。
- v0.16.0：Store 关闭态、生命周期/巡检独占锁、GET Bucket 与
  HEAD Content-Length。
- v0.17.0：静态加密升级为分块 AES-256-GCM（认证加密）。
- v0.18.0：长任务上下文取消贯穿、Release 乱序测试、客户端 godoc 示例。
- v0.19.0：稳定性收口（乱序×3、全量门禁、发布检查单）。

> 当前状态：**v1.2.0**。

文档索引见 [docs/README.md](docs/README.md)。

## 目录

```
filex/
├── docs/
│   ├── README.md          # 文档索引
│   ├── architecture.md    # 架构详解（布局/原子性/并发/安全）
│   ├── api.md             # API 定版
│   └── operations.md      # 运维手册
├── proto/                 # 协议常量、线格式与 Range 解析
├── server/                # 协议服务端（http.Handler，可挂 webx）
├── client/                # 协议客户端（可注入 httpx）
├── examples/
│   ├── basic/             # 引擎基础用法（独立模块）
│   ├── protocol/          # 服务端 + 客户端端到端（独立模块）
│   └── http3/             # webx + httpx HTTP/3 端到端（独立模块）
├── ERRORS.md              # 错误码手册
├── BENCHMARKS.md          # 基准测试方法与结果
└── README.md
```

## 协议快速上手（v0.2.0）

```go
import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/filex/server"
)

func main() {
	store, err := filex.New(filex.Config{DataDir: "./data"})
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	go func() { _ = http.ListenAndServe(":8099", server.NewHandler(server.HandlerConfig{Store: store})) }()

	c, err := client.New("http://127.0.0.1:8099")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "my-bucket")
	_, _ = c.Put(ctx, "my-bucket", "hello.txt",
		strings.NewReader("你好，filex"), filex.PutOptions{})
}
```

## License

MIT © [lcylpzls](https://github.com/lcylpzls)
