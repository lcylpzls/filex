# 更新日志

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
