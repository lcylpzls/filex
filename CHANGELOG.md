# 更新日志

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
