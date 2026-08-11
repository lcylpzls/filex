# 贡献指南

## 语言

- 代码注释、文档、日志与提交信息一律使用简体中文。

## 提交规范

遵循 Conventional Commits：

- `feat`：新功能（同步递增版本）；
- `fix`：缺陷修复；
- `docs`：文档；
- `style`：格式；
- `ci`：CI/发布流程；
- `refactor`：重构（行为不变）。

示例：`feat(filex): v0.12.0 分片会话 TTL 清理`

## 质量门禁（提交前必须全绿）

```powershell
go test -count=1 -shuffle=on ./...
go test -count=1 -coverprofile=coverage.out ./...   # 四包 100%
go test -race -count=1 ./...
go vet ./... && staticcheck ./...
go test -run '^$' -fuzz '^Fuzz' -fuzztime=5s .
govulncheck ./...
gofmt -l .   # 必须为空
```

## 发布流程

- 版本号遵循语义化版本；
- 主分支合并后由 tag 触发 Release 工作流；
- 1.0 及以后版本的发布由维护者显式确认。

## 分支与评审

- 直接推 `main` 仅限自动化发布与维护者；
- 新功能建议先更新 `docs/architecture.md` 与 CHANGELOG 再动代码。
