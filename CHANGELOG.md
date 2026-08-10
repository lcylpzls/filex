# 更新日志

## [v0.22.0] - 2026-08-10

### 变更

- 家族测试底座接入：根包与全部子包测试改用语义等价的
  testx 断言（含 Require* 致命断言），消除 mustBucket/mustPut 等
  手写断言辅助；
- 测试依赖新增 `testx v1.2.0`，errx 同步升级 v1.4.0。

### 质量

- 根包语句覆盖率 100%；race / vet / staticcheck 全绿。

## [v0.21.0] - 2026-08-10

### 变更

- examples 子模块升级 webx → `github.com/lcylpzls/webx/v2 v2.0.1`
  （HTTP/3 示例同步迁移模块路径）。

## [v0.20.0] - 2026-08-10

### 新增

- `TraceHook` 链路追踪钩子（零依赖接口 + `Config.TraceHook`）：
  对象操作（Put/Get/Head/Delete/List/Copy/Move）自动埋点
  （filex.operation / filex.bucket / filex.key 属性），
  由 tracex 等外部适配器接入；
- 对象操作追踪测试，根包与子包覆盖率保持 100%。

## [v0.19.0] - 2026-08-10

### 收口

- 稳定性验证：根包与示例 `-shuffle=on -count=3` 全绿；
- 全量质量门禁（全平台 race / vet / staticcheck / fuzz / govulncheck）
  通过；
- 终审文档补充发布检查单，1.0 候选确认。

## [v0.18.0] - 2026-08-10

### 改进

- 上下文取消贯穿 `RunLifecycle` / `SweepOrphans` / `List`，
  长任务可被取消并归一 `filex_cancelled`；
- Release 工作流测试同样启用乱序（`-shuffle=on`）；
- 客户端 godoc 示例（ExampleClient_Put）。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.17.0] - 2026-08-10

### 安全升级

- 静态加密由 AES-256-CTR 升级为**分块 AES-256-GCM**（64 KiB/块）：
  - 认证加密：篡改/截断密文可被即时发现；
  - 流式实现，内存占用有界（单块 + 少量开销）；
  - 文件随机数存入元数据，数据文件仅含密文块；
- 兼容说明：v0.5.x–v0.16.x 的 CTR 加密对象需重新写入后再读取
  （格式升级，pre-1.0 变更）。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.16.0] - 2026-08-10

### 新增

- Store 关闭态：`Close` 后所有操作返回 `filex_closed`，重复关闭幂等；
- 生命周期清理与孤儿巡检改为独占锁，消除清理与写入/读取并发的
  文件删除竞争；
- 协议新增 `GET /filex/v1/buckets/{bucket}`（桶信息），客户端
  `GetBucket`；
- HEAD 对象响应补 `Content-Length`。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.15.0] - 2026-08-10

### 新增

- 桶元数据解码 fuzz 目标（FuzzDecodeBucketMeta）接入 CI；
- API 冻结快照更新至 v0.15.0；
- 终审文档同步。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.14.0] - 2026-08-10

### 修复

- 版本化迁移缺陷：先建对象再开启版本化时，历史扁平对象现在可读、可枚举，
  删除标记移除后可回退（修复旧对象“消失”问题）。

### 新增

- `BucketStats`：对象数 / 版本数 / 字节用量统计；
- `docs/operations.md` 运维手册（备份恢复、巡检、健康、安全、升级）。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.13.0] - 2026-08-10

### 改进

- 客户端自动设置 `Content-Length`（Len / Seeker 探测），服务端可提前
  感知长度；
- 新增并发读写基准（BenchmarkPutConcurrent / BenchmarkGetConcurrent）；
- BENCHMARKS.md 补充并发数据。

### 质量

- 四包覆盖率保持 100%；race（全平台）/ vet / staticcheck / fuzz /
  govulncheck 全绿。

## [v0.12.0] - 2026-08-10

### 新增

- 分片会话 TTL：`Config.UploadTTL`（默认 24 小时），孤儿巡检清理过期会话；
- `DeleteBucket` 拒绝删除存在活动分片上传的桶；
- CI 工业化：全平台 race、测试乱序（`-shuffle=on`）防顺序依赖；
- 仓库工业化：CONTRIBUTING / CODEOWNERS / Issue 模板 / godoc 示例；
- 测试桩并发安全修复（fakeLogger / fakeMetrics）。

### 质量

- 四包覆盖率保持 100%；race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.11.0] - 2026-08-10

### 新增

- 数据完整性审计：
  - `VerifyObject`：单对象流式复验 SHA256；
  - `VerifyAll`：并发审计全部桶的当前对象与全部版本（跳过删除标记），
    输出损坏清单；
- `BucketUsage` 公开桶用量（含全部非删除版本）；
- 上下文取消语义：Put / UploadPart / CompleteMultipartUpload 在读取与
  合并阶段响应取消/超时，归一为 `filex_cancelled`；
- `Metrics` 接口明确要求实现必须并发安全。

### 质量

- 四包覆盖率保持 100%；race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.10.0] - 2026-08-10

### 改进

- 协议响应头新增 `X-Filex-Version-ID`，`GetVersion` / `HeadVersion`
  客户端可拿到版本 ID；
- 新增 [docs/final-review.md](docs/final-review.md) 1.0 候选终审；
- API 冻结快照更新至 v0.10.0。

### 结论

- filex 达到 1.0 候选标准；**v1.0.0 是否发布由用户决定**。

## [v0.9.0] - 2026-08-10

### 改进

- 条件请求语义明确化：
  - `If-None-Match` 命中 → `filex_not_modified`（HTTP 304）；
  - `If-Match` 不匹配 → `filex_precondition_failed`（HTTP 412）；
  - 客户端 `Get` 直接返回对应 errx 错误码，不再退化为泛化错误；
- 错误码手册与设计文档同步新增两个错误码。

### 质量

- 四包覆盖率保持 100%；race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.8.0] - 2026-08-10

### 修复

- `SweepOrphans` 不再清理活动分片上传会话目录 `.uploads`；
- 补充活动会话保护测试。

### 文档

- 架构文档更新：版本化布局、静态加密数据格式、生命周期与孤儿巡检；
- 文档索引补齐错误码手册、基准与 API 冻结快照入口。

### 质量

- 四包覆盖率保持 100%；race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.7.0] - 2026-08-10

### 新增

- HTTP/3 端到端示例：webx 服务端 + httpx 客户端承载 filex 协议，
  测试断言真实协商 `HTTP/3.0`；
- `ERRORS.md` 错误码手册定稿；
- `docs/api-v0.7.0.md` API 冻结快照；
- 路线图 v0.1.0 → v0.7.0 全部完成。

### 质量

- 四包覆盖率均 100%；race / vet / staticcheck / fuzz / govulncheck 全绿；
- 三平台 CI + Linux 多发行版矩阵 + HTTP/3 示例全绿。

## [v0.6.0] - 2026-08-10

### 新增

- 生命周期管理：
  - `SetBucketLifecycle`（ExpireDays / MaxVersions）与 `RunLifecycle`
    （过期清理 + 版本收敛，支持版本化桶）；
- 孤儿巡检：`SweepOrphans` 清理孤儿数据、临时文件与空版本目录；
- 健康检查：`Store.Health` 与协议端点 `GET /filex/v1/health`，
  客户端 `Health`；
- 服务端请求指标：`HandlerConfig.Metrics` 统计成功/错误请求；
- 基准测试（Put/Get/List）与 `BENCHMARKS.md`；
- Bucket 线格式携带生命周期字段。

### 质量

- 根包与 proto / server / client 语句覆盖率均 100%；
  race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.5.0] - 2026-08-10

### 新增

- 服务端静态加密：AES-256-CTR 逐对象加密，DEK 随机生成并用 AES-GCM
  主密钥（KEK）包装存入元数据；读取自动解密，SHA256 仍基于明文；
- 加密对象拒绝 Range 读取；篡改密文可在校验读取时发现；
- 鉴权：`Bearer` 令牌、HMAC-SHA256 请求签名、鉴权回调三种方式；
- 防重放：开启鉴权后要求 `X-Filex-Timestamp`（±5 分钟窗口）；
- 审计：`HandlerConfig.Audit` 回调输出请求 ID、主体、路径与状态；
- `Config.MaxParts` 部件数量上限可配置（默认 10000）；
- 客户端 `WithHMAC` 请求签名选项。

### 质量

- 根包与 proto / server / client 语句覆盖率均 100%；
  race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.4.0] - 2026-08-10

### 新增

- 桶版本化与软删除：
  - `SetBucketVersioning` / `GetVersion` / `HeadVersion` /
    `DeleteVersion` / `RestoreVersion` / `ListVersions`；
  - 删除转为删除标记，历史版本可枚举、可读、可恢复、可永久删除；
  - 版本化桶的 List/Head/Get 自动指向最新可见版本；
- 对象管理：`Copy` / `Move`（保留内容类型与元数据）；
- 桶配额：`SetBucketQuota`，写入与分片完成时检查并回滚超限对象；
- 协议层：bucket `versioning` / `quota` 查询参数；对象
  `version-id` / `versions=true` / `copy` / `move` / `restore` 端点；
  Bucket/Object 线格式携带版本化、配额与版本 ID；
- 客户端同名方法：`SetBucketVersioning` / `SetBucketQuota` /
  `GetVersion` / `HeadVersion` / `DeleteVersion` / `RestoreVersion` /
  `ListVersions` / `Copy` / `Move`；
- 分片上传支持版本化桶与配额回滚。

### 质量

- 根包与 proto / server / client 语句覆盖率均 100%；
  race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.3.0] - 2026-08-10

### 新增

- 引擎分片上传：
  - `InitiateMultipartUpload` / `UploadPart` / `CompleteMultipartUpload` /
    `AbortMultipartUpload` / `ListParts`；
  - 部件级 SHA256、乱序上传、同部件幂等覆盖、断点续传（会话保留）；
  - 合并时校验部件连续性、大小与总数上限，成功后原子提交并清理会话；
- 协议层分片端点（`upload=initiate|part|complete`、`upload=parts`、
  `upload=abort`）与线格式（UploadInfo / PartInfo / PartList）；
- 客户端分片方法 + `PutMultipart`：自动切分、并发上传、失败自动中止；
- 会话/部件元数据 fuzz 目标接入 CI。

### 质量

- 根包与 proto / server / client 语句覆盖率均 100%；
  race / vet / staticcheck / fuzz / govulncheck 全绿；
- CI fuzz 单目标时长调整为 5s，消除上下文超时抖动。

## [v0.2.0] - 2026-08-10

### 新增

- `filex/proto`：协议常量、JSON 线格式（Bucket/Object/List/Error）、
  `ParseRange` 范围解析（含后缀范围与越界截断）；
- `filex/server`：自研协议 HTTP 处理器（`http.Handler`，可挂 webx）：
  - 桶：创建/枚举/Head/删除；
  - 对象：流式 Put/Get/Head/Delete、分页列表（prefix/marker/limit/
    delimiter/common prefixes）；
  - Range 读取（206 + Content-Range）、条件请求（If-Match /
    If-None-Match → 412/304）；
  - 统一错误 JSON（code/kind/message/requestId）、请求 ID、
    panic 恢复与结构化日志；
- `filex/client`：协议客户端（可注入 `http.Client` / httpx、Bearer
  令牌），桶与对象全生命周期、范围读取、校验读取、错误码还原；
- 引擎 `GetOptions` 新增 `Range` / `IfMatch` / `IfNoneMatch`；
- 端到端示例 `examples/protocol`（服务端 + 客户端）；
- `FuzzParseRange` 接入 CI。

### 质量

- 根包与 proto / server / client 语句覆盖率均 100%；
  race / vet / staticcheck / fuzz / govulncheck 全绿。

## [v0.1.0] - 2026-08-10

### 新增

- 核心引擎 `filex.Store`：
  - `CreateBucket` / `DeleteBucket` / `ListBuckets`；
  - `Put` / `Get` / `Head` / `Delete` / `List`；
  - 原子写入（临时文件 + fsync + rename）、SHA256 流式校验与 ETag；
  - 对象元数据（ContentType / 自定义 Metadata / 创建与修改时间）；
  - 分页列表（prefix / marker / limit / delimiter / common prefixes）；
  - 桶名校验、键名校验、大小上限；
- errx 错误码全集（filex_*），logx 结构化日志，Metrics 接口；
- 并发安全：分片锁 + 原子替换，支持并发读写与并发上传同一对象；
- fuzz 目标（桶名/键名/元数据解码）接入 CI；
- Linux 多发行版容器矩阵 + 三平台 CI + Release 工作流。

### 质量

- 根包语句覆盖率 100%；race / vet / staticcheck / govulncheck 全绿；
- 示例 `examples/basic` 独立模块，三平台 CI 覆盖。
