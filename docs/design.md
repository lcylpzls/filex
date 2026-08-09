# filex 设计定版

> 版本：v0.0.0（规划定稿） · 状态：文档已定版，代码未开始

## 1. 定位

filex 是**自用对象存储 Go 组件**：提供本地盘对象存储引擎、
自研协议服务端与客户端，嵌入业务进程即可获得
「Bucket → 对象 → 元数据 → 完整性 → 传输」的完整闭环。

## 2. 范围边界（明确不做）

| 不做 | 原因与替代 |
| --- | --- |
| S3 / OSS / WebDAV 兼容 | 自用两端同源，无第三方协议需求 |
| 多节点分布式一致性 | 自用单机/单盘，备份与迁移由文件系统层面解决 |
| 纠删码 / 跨地域复制 | 超出自用规模 |
| 独立守护进程 / Web 控制台 | filex 是库；管理面由业务程序或示例提供 |
| 冷热分层 / 对象压缩 | 后续按需引入，不在 v1 承诺内 |
| 对象文件系统挂载（FUSE） | 不属于对象存储语义 |

## 3. 核心概念

| 术语 | 含义 |
| --- | --- |
| Bucket | 命名空间：对象容器，创建/删除/枚举，名称全局校验 |
| Object | 不可变数据单元：键 + 内容 + 元数据 + 完整性信息 |
| ObjectInfo | 对象元数据快照：大小、SHA256/ETag、内容类型、时间戳、自定义元数据 |
| Store | 本地盘引擎：落盘、读取、枚举、删除、原子写 |
| Handler | 自研协议 HTTP 处理器（`http.Handler`，可挂 webx） |
| Client | 自研协议客户端（默认标准库传输，可换 httpx） |
| UploadID / Part | 分片上传：一次上传的会话与部件（v0.3.0） |
| VersionID | 版本化对象的版本标识（v0.4.0） |

## 4. 自研协议 v1 总览

- 传输：HTTP/1.1、HTTP/2、HTTP/3（TLS 必须；示例默认 HTTP/3）；
- 控制面：JSON（`application/json`）；数据面：原始二进制 body；
- 端点前缀：`/filex/v1`；
- 命名空间：`/filex/v1/buckets/{bucket}`、
  `/filex/v1/buckets/{bucket}/objects/{key}`；
- 对象键可含 `/` 等任意 UTF-8 字符（除 NUL），协议内使用 URL 转义；
- 完整性：写入方可选携带 `X-Filex-Sha256`，服务端写入时强制计算并校验；
  读取响应固定返回 `X-Filex-Sha256`（对象内容 SHA256）；
- 条件请求：`If-Match` / `If-None-Match`（ETag）；
- 范围读取：`Range: bytes=start-end`，响应 `Content-Range`；
- 列表：`prefix` / `marker` / `limit` / `delimiter`，返回对象与公共前缀；
- 分片上传：`PUT` 对象端点携带 `upload=initiate|part|complete`，
  `GET` 携带 `upload=parts` 枚举部件，`DELETE` 携带 `upload=abort`
  中止会话；会话 ID 与部件号通过查询参数传递；
- 错误：统一 JSON `{"code","kind","message","requestId"}`，
  HTTP 状态由 errx Kind 映射；
- 鉴权：v0.5.0 起支持 `Authorization: Bearer` 或令牌回调注入。
- 静态加密：v0.5.0 起支持 AES-256-CTR 逐对象加密，DEK 随机生成、
  由 AES-GCM 主密钥包装；SHA256 校验基于明文。

## 5. 数据流

```
Put(ctx, bucket, key, body, opts)
  → 校验 bucket/key/大小
  → 写临时文件（流式 SHA256 + 可选期望校验）
  → fsync 临时文件 → rename 为 data
  → 写 meta.json（临时文件 + rename）
  → 返回 ObjectInfo{ETag=SHA256}

Get(ctx, bucket, key, opts)
  → 读 meta.json（校验 JSON 与关键字段）
  → 打开 data → 返回流式 Reader（可选中途/末尾复验 SHA256）

List(ctx, bucket, opts)
  → 扫描 objects/*/meta.json
  → 按 key 排序 → 过滤 prefix/marker → 分页 → 聚合 delimiter 公共前缀
```

## 6. 完整性模型

- 写入：SHA256 必算；`PutOptions.ExpectedSHA256` 提供时校验，不符即失败；
- 读取：`GetOptions.Verify` 开启时流式复算，EOF 时校验；ETag 即 SHA256；
- 元数据：JSON 解码失败或关键字段缺失 → `filex_metadata_corrupt`；
- 原子性：data 先就绪，meta 后生效；meta 存在即视为对象存在。

## 7. 错误码（errx）

| 错误码 | 含义 | Kind | 建议 HTTP |
| --- | --- | --- | --- |
| `filex_invalid_config` | 配置非法 | invalid_argument | 400 |
| `filex_invalid_bucket` | 桶名非法 | invalid_argument | 400 |
| `filex_invalid_key` | 键名非法 | invalid_argument | 400 |
| `filex_bucket_exists` | 桶已存在 | already_exists | 409 |
| `filex_bucket_not_found` | 桶不存在 | not_found | 404 |
| `filex_bucket_not_empty` | 桶非空 | conflict | 409 |
| `filex_object_not_found` | 对象不存在 | not_found | 404 |
| `filex_object_too_large` | 对象超过大小上限 | invalid_argument | 400 |
| `filex_checksum_mismatch` | SHA256 校验失败 | data_loss | 500 |
| `filex_metadata_corrupt` | 元数据损坏 | data_loss | 500 |
| `filex_storage_failed` | 存储 IO 失败 | unavailable | 503 |
| `filex_invalid_range` | 范围非法 | invalid_argument | 400 |
| `filex_internal` | 服务器内部错误 | internal | 500 |
| `filex_unauthorized` | 未认证 | unauthorized | 401 |
| `filex_forbidden` | 无权限 | forbidden | 403 |
| `filex_not_modified` | 对象未修改（304 语义） | conflict | 304 |
| `filex_precondition_failed` | 前置条件不满足（412 语义） | conflict | 412 |
| `filex_upload_not_found` | 分片会话不存在 | not_found | 404 |
| `filex_upload_invalid` | 分片参数非法 | invalid_argument | 400 |
| `filex_upload_incomplete` | 分片不完整 | invalid_argument | 400 |
| `filex_version_not_found` | 版本不存在 | not_found | 404 |
| `filex_quota_exceeded` | 配额超限 | quota_exceeded | 429 |

## 8. 可观测性

- 日志：`logx.Logger` 注入，操作级结构化事件（bucket/key/bytes/耗时/结果）；
- 指标：`Metrics` 接口注入，计数 Put/Get/Delete/List 与字节量、错误分类；
- 审计：v0.5.0 起记录鉴权主体、操作、对象与结果。

## 9. 质量门禁

每版发布前必须全绿：

```powershell
go test -count=1 ./...
go test -count=1 -coverprofile=coverage.out ./...   # 根包 100%
go test -race -count=1 ./...
go vet ./... && staticcheck ./...
go test -run '^$' -fuzz '^Fuzz' -fuzztime=10s .
govulncheck ./...
```

CI：ubuntu/windows/macos 三平台 + Linux 多发行版容器矩阵
（debian / fedora / rockylinux / archlinux / alpine）+ fuzz + vulncheck
+ Release（tag 触发）。
