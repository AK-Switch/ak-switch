# CONTRIBUTING.md — AK Switch 开发指南

## 构建

```bash
# 编译安装到 GOPATH/bin
go install ./cmd/akswitch/

# 或使用 Makefile
make build
```

## 测试

测试按速度分层，用 `//go:build` 标签区分：

| 层级 | 标签 | 上限 | 命令 |
|------|------|------|------|
| 单元 | `unit` | <=1s | `make test-unit` |
| 集成 | `integration` | <=10s | `make test-integration` |
| E2E | `e2e` | <=2m | `make test-e2e` |

```bash
# 全量测试
make test-all

# 或单独运行
go test -tags=unit -count=1 -short ./internal/...
go test -tags=integration -count=1 -race ./
go test -tags=e2e -count=1 -timeout=5m -race ./
```

## 开发环境

- Go 1.23+（构建使用 Go 1.26）
- 主要开发平台：Windows
- 跨平台：Linux、macOS（通过 GitHub Actions CI 验证）
- 无需外部依赖（纯 Go 标准库 + Cobra CLI + Prometheus client）

## 分支策略

- `main` — 稳定分支，受保护，禁止直接推送
- 功能分支：`feature/xxx`、`bugfix/xxx`、`docs/xxx`
- 遵循 GitHub Flow：功能分支 → Draft PR → 合并到 main

## 提交规范

```
类型: 简短描述（不超过 50 字）
```

| 类型 | 含义 |
|------|------|
| `feat` | 新功能 |
| `fix` | Bug 修复 |
| `refactor` | 重构（不改变功能） |
| `docs` | 文档变更 |
| `chore` | 杂务（构建、配置、CI） |

## PR 流程

1. 从 main 创建功能分支
2. 原子提交，确保 `make test-all` 通过
3. 提交 **Draft PR**，标题写明改动内容
4. 仓库启用 Auto-merge（squash），CI 通过后自动合并
5. 合并后删除功能分支

## 文档纪律

- 修改 CLI 命令/标志 → 同步更新 `docs/cli-reference.md`
- 新增配置字段 → 同步更新 `docs/configuration.md`
- 新增 API 端点 → 同步更新 `docs/api.md`
- 发版 → 更新 `CHANGELOG.md`

## 发版

```bash
make release VERSION=v0.x.x
# 等价于 git tag v0.x.x && git push origin v0.x.x
```

GitHub Actions 自动构建 8 平台二进制 + 创建 Release。