# filex 版本路线

> 目标：v0.1.0 起每版完成即全自动 CI + Release，全部通过后进入下一版；
> 定版级质量贯穿全程（100% 覆盖、race、fuzz、三平台 CI、govulncheck）。

## v0.1.0 — 核心引擎

- Bucket/Object 生命周期：CreateBucket / DeleteBucket / ListBuckets /
  Put / Get / Head / Delete / List；
- 原子写入、SHA256 流式校验与 ETag、元数据；
- 分页列表（prefix / marker / limit / delimiter / common prefixes）；
- errx 错误码全集、logx 结构化日志、Metrics 接口；
- 并发安全（分片锁）、孤儿数据清理；
- fuzz：桶名/键名/元数据解码；三平台 CI + Linux 多发行版矩阵 + Release。

> 状态：**已发布**（v0.1.0，2026-08-10）。根包语句覆盖率 100%，
> race / vet / staticcheck / fuzz / govulncheck 全绿，三平台 CI 与
> Linux 多发行版矩阵已接入。

## v0.2.0 — 自研协议服务端与客户端

- `filex/server`：HTTP 处理器（标准 `http.Handler`，可挂 webx）；
- `filex/client`：协议客户端（标准库传输，可选 httpx）；
- 流式 Put/Get、Range 读取、条件请求（If-Match / If-None-Match）；
- 列表协议端点与分页、统一错误 JSON、请求 ID；
- 端到端示例：net/http 服务端 + 客户端。

> 状态：**已发布**（v0.2.0，2026-08-10）。root / proto / server /
> client 四包覆盖率均 100%，三平台 CI 与 Linux 多发行版矩阵全绿。

## v0.3.0 — 分片上传与断点续传

- Initiate / UploadPart / Complete / Abort；
- 部件级 SHA256、乱序上传、并发部件、断点续传；
- 服务端部件清理、上传会话过期回收；
- 分片上传客户端自动切分（阈值默认 16 MiB）。

> 状态：**已发布**（v0.3.0，2026-08-10）。会话/部件元数据 fuzz 接入，
> 四包覆盖率均 100%，三平台 CI 全绿。

## v0.4.0 — 版本化与对象管理

- 版本化桶：VersionID、ListVersions、Get/Delete 指定版本、恢复；
- Copy / Move（同桶与跨桶）；
- 桶配额与元数据、DeleteBucket(Force)；
- 软删除与回收站语义。

> 状态：**已发布**（v0.4.0，2026-08-10）。版本化/软删除/Copy/Move/
> 配额全部落地，四包覆盖率均 100%。

## v0.5.0 — 安全

- 服务端加密：每对象 AES-GCM 密钥 + 主密钥包装（KEK）；
- 鉴权：Bearer 令牌 / 令牌回调 / HMAC 请求签名；
- 审计日志：主体、操作、对象、结果、请求 ID；
- 上传大小/部件数量上限、防重放（时间戳窗口）。

## v0.6.0 — 生命周期与可观测

- 过期清理（按年龄/保留版本数）；
- 健康检查端点、指标完备化（请求维度/字节维度/错误分类）；
- 孤儿数据主动巡检与清理；
- 基准测试（本地盘顺序读写、并发、大对象）。

## v0.7.0 — 发布前终审

- webx + httpx HTTP/3 端到端示例；
- README / SECURITY / ERRORS / LICENSE 定稿；
- API 冻结与文档快照；roadmap 收口。

## v0.8.0+ — 自检打磨

roadmap 完成后继续自我检查、修复边界、补充测试与文档，
逐版推进至自评达到 1.0 候选；**1.0 是否发布由用户决定**。

## 质量门禁（每版）

```powershell
go test -count=1 ./...
go test -count=1 -coverprofile=coverage.out ./...   # 根包 100%
go test -race -count=1 ./...
go vet ./... && staticcheck ./...
go test -run '^$' -fuzz '^Fuzz' -fuzztime=10s .
govulncheck ./...
```

CI：ubuntu/windows/macos 三平台 + Linux 多发行版容器矩阵
（debian / fedora / rockylinux / archlinux / alpine）+ fuzz job
+ govulncheck job + Release（tag 触发）。
