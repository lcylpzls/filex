# 跨区域异步镜像

## 定位

`filex/replication` 面向“一个源区域单写、另一个区域保存灾备副本”的场景。
目标端不作为第二写入点，不处理多主冲突。未启用该包时，既有 filex
读写、版本化和生命周期行为不变。

## 最小用法

```go
source, err := client.New("https://center-a.example.com",
	client.WithToken("source-token"))
if err != nil {
	panic(err)
}
target, err := client.New("https://center-b.example.com",
	client.WithToken("target-token"))
if err != nil {
	panic(err)
}

replicator, err := replication.New(replication.Config{
	Source:    source,
	Target:    target,
	StateDir:  "/var/lib/filex-replication",
	Buckets:   []string{"center-backups"},
	Interval:  30 * time.Second,
	RetryBase: 2 * time.Second,
	RetryMax:  5 * time.Minute,
})
if err != nil {
	panic(err)
}
go func() {
	_ = replicator.Run(ctx)
}()
```

`Source` 和 `Target` 也接受同进程的 `*filex.Store`。跨区域部署通常使用
`*client.Client`，服务端由 `server.NewHandler` 承载。

## 一致性语义

- 每轮 `Mirror` 枚举源端和目标端全部物理对象与版本；
- 复制身份为 `bucket + key + versionID`，非版本化对象使用空版本 ID；
- 版本 ID、SHA256、内容类型、自定义元数据和创建/更新时间原样保留；
- 删除标记作为版本复制，目标端当前视图与源端一致；
- 源端没有的目标对象或版本会被删除，因此目标桶必须专用；
- 同一版本 ID 已存在但不一致时拒绝覆盖，任务进入 `failed`；
- 内容读取与目标写入均校验 SHA256，重复执行相同任务保持幂等。

## 任务与恢复

任务保存在 `StateDir/tasks/*.json`，状态如下：

| 状态 | 含义 |
| --- | --- |
| `pending` | 等待执行或等待退避时间 |
| `running` | 执行中；进程异常退出后重启恢复为 `pending` |
| `replicated` | 当前目标和源端一致 |
| `failed` | 最近一次执行失败，记录错误与下次尝试时间 |

失败按 `RetryBase × 2^(attempts-1)` 指数退避，并以 `RetryMax` 封顶。
目标恢复后调用 `Replay` 清除失败退避并立即全量对账。

```go
report, err := replicator.Replay(ctx)
stats, statsErr := replicator.Stats(ctx)
tasks, tasksErr := replicator.Tasks(ctx)
```

`Stats.ReplicationLag` 是当前时间与最近一次成功任务的时间差；存在大量新
待复制对象时，还应结合 `Pending` 和业务 RPO 监控。

## 可观测

- `Report`：本轮扫描、复制、删除、跳过和失败数量；
- `Stats`：pending / running / replicated / failed、最近成功时间和延迟；
- `Logger`：桶级和任务级失败日志；
- `Metrics`：pending、failed、lag、last_success 与执行耗时；
- `TraceHook`：每个任务的桶、键、版本和动作。

## 明确边界

- 不复制 PostgreSQL 数据库；
- 不支持双主写入冲突合并；
- 不复制桶配额与生命周期配置；
- 不传播桶删除，源桶缺失时保留目标并持续报错；
- 不依赖外部数据库、容器或 Kubernetes；
- 目标端内容被人工修改后不会静默覆盖同一版本冲突，需要人工修复后重放。
