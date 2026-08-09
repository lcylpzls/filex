# 更新日志

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
