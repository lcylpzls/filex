# 1.0 候选终审

> 结论：filex 已具备 1.0 候选条件。**是否发布 v1.0.0 由用户决定。**

## 自检清单

| 维度 | 状态 |
| --- | --- |
| 功能闭环 | Bucket/Object 全生命周期、分片上传、版本化/软删除、Copy/Move、配额、加密、生命周期、孤儿巡检 |
| 协议 | 自研 filex 协议 v1，HTTP/1.1/2/3 承载，JSON 控制面 + 二进制数据面 |
| 安全 | AES-256-CTR 静态加密 + DEK 主密钥包装；Bearer/HMAC/回调鉴权；防重放；审计 |
| 可靠性 | 原子写入、SHA256 完整性、删除标记、回滚、分片断点续传 |
| 可观测 | 结构化日志、请求指标、健康检查、请求 ID、审计事件 |
| 跨平台 | Windows/macOS/Linux 三平台 CI + Linux 多发行版容器矩阵 |
| 测试 | 四包语句覆盖率 100%；race / vet / staticcheck / fuzz / govulncheck 全绿 |
| 端到端 | net/http 与 webx+httpx HTTP/3 双示例 |
| 文档 | README / design / architecture / api / roadmap / ERRORS / SECURITY / BENCHMARKS / 终审 |
| 依赖 | 核心仅 errx + logx；示例隔离在独立模块 |

## 已知限制（自用可接受）

- `List` 为全量扫描 + 内存排序，万级对象内可接受；
- 加密对象不支持 Range 读取；
- `PutMultipart` 内存占用约 concurrency × partSize；
- 配额检查为扫描式，并发写入下允许短暂超限；
- 单机单盘模型，无多节点复制与纠删码；
- 分片会话期间的部件明文暂存，最终对象静态加密；
- 软删除后对象不可见，历史版本需显式访问/恢复。

## 1.0 候选标准

- API 已冻结（见 [api-v0.10.0.md](api-v0.10.0.md)）；
- 全部规划与自检打磨完成（v0.1.0 → v0.10.0）；
- 无已知 P0/P1 问题；发布前仅需用户确认。
