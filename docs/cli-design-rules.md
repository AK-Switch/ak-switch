# CLI Design Rules

本文档记录 AK-Switch CLI 的设计决策和 rationale。修改 CLI 前先读 AGENTS.md 中的规则，再回来查本文档了解背景。

## 设计哲学

CLI 是用户与系统交互的主要入口。好的 CLI 不需要用户思考"这里该用什么语法"——命令组织应该形成肌肉记忆，而不是每次都要查文档。

## 命令组织

### 为什么 verb-first

AK-Switch 采用 `akswitch <group> <subcommand> [args]` 模式。group 是资源类型（provider / key / config），subcommand 是动作（add / list / remove）。

参照物：`kubectl get pods`、`docker container ls`、`cloudflared tunnel info`。

动作在前的好处：用户形成肌肉记忆后，想做什么直接想动词，不需要先想"我要操作的是哪个资源类型"。

### 为什么 provider 是 args[0] 而不是 flag

`akswitch status sensenova` 比 `akswitch status --provider sensenova` 更短、更符合直觉。provider 名是用户关心的对象，不是上下文筛选条件。

例外：`config get/set` 以 key 为主参数，provider 是可选上下文，所以 provider 跟在后面。这是语义驱动的例外，不是不一致。

### 为什么不做动态子命令

`akswitch sensenova status` 看起来很美，但实现上需要：
- 从 config.toml 读取 provider 列表
- 动态注册 Cobra 子命令
- 与 `config reload` 热更新机制纠缠（provider 变了，命令树要不要更新？）
- 测试复杂度陡增（需要 mock config.toml 才能测试命令注册）

收益（少敲几个字符）不够大，代价（动态命令树 + 测试复杂度）不小。

## 配置字段原则

### GenaiBase 的矛盾修复

`GenaiBase` 字段在 server 层有实际用途（`/genai/` 路径路由），不是死代码。矛盾在于三处不一致：

- `provider add` flag 文档：`(optional)`
- `config.go` 结构体注释：`// Generative AI base URL (required)`
- `Validate()`：检查 `GenaiBase == ""` 并报 required 错误

修复：flag 文档改为 `(required)`，`provider add` 添加验证，三处统一。

### 为什么 required 验证必须在添加时完成

`provider add` 时只验证 `--target`，但 `Validate()` 在启动时报 `GenaiBase` 缺失。用户在 `add` 阶段以为成功了，到 `start` 阶段才报错——两次操作之间的时间差可能是几天，用户完全不记得自己少传了一个参数。

规则：如果一个字段在 `Validate()` 中检查为 required，`provider add` 必须在写入前检查。不存在"添加时可选、启动时必填"的字段。

## Provider 命名

Provider 名只允许 `[a-z0-9-]`。大写字母会导致：
- TOML 解析时的大小写敏感问题
- URL 路径中的编码不一致
- shell 脚本中的引号问题

在 `provider add` 时检查并警告，而不是在启动时报错——早点发现，成本更低。

## 错误信息

错误信息回答两个问题：
1. 出了什么问题
2. 怎么解决

`--target/-t is required` 包含问题（--target 缺失）和解决方案（传 --target）。`invalid provider config` 只说有问题，没说怎么修。

## 参照物

| 项目 | 学习点 |
|------|--------|
| `cloudflared` | verb-first 子命令，资源管理 + 守护进程模式 |
| `kubectl` | 动词一致性的标杆，筛选用 flag |
| `docker` | 简单操作短名字（`docker ps`），复杂操作用子命令（`docker container ls`） |
| `gh` (GitHub CLI) | `gh issue list --label bug`，筛选用 flag |
| `aws` | verb-first，资源作为 args，可选参数用 flag |

## 历史决策

| 日期 | 决策 |
|------|------|
| 2026-07-29 | 确立 verb-first 命令组织模式 |
| 2026-07-29 | `status` 接受可选 provider 参数 |
| 2026-07-29 | 不做动态子命令 |
| 2026-07-29 | required 验证必须在添加时完成 |
| 2026-07-29 | `GenaiBase` 保留功能，修复 required 矛盾（flag 文档 + 添加时验证） |
