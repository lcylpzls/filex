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
- v0.5.0：服务端静态加密（AES-256-CTR + DEK 主密钥包装）、
  Bearer/HMAC/回调鉴权、防重放与审计。
- v0.6.0：生命周期（过期清理/版本收敛）、孤儿巡检、健康检查、
  服务端请求指标与基准。

详细规划见 [docs/roadmap.md](docs/roadmap.md)，设计见 [docs/design.md](docs/design.md)。

## 目录

```
filex/
├── docs/
│   ├── README.md          # 文档索引
│   ├── research.md        # 竞品调研与自研取舍
│   ├── design.md          # 设计定版（定位/范围/协议/错误码）
│   ├── architecture.md    # 架构详解（布局/原子性/并发/安全）
│   ├── api.md             # API 定版
│   └── roadmap.md         # 版本路线
├── proto/                 # 协议常量、线格式与 Range 解析
├── server/                # 协议服务端（http.Handler，可挂 webx）
├── client/                # 协议客户端（可注入 httpx）
├── examples/
│   ├── basic/             # 引擎基础用法（独立模块）
│   └── protocol/          # 服务端 + 客户端端到端（独立模块）
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
