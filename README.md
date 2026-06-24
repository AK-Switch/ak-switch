# ⚡ Alvus — Multi-Instance Fork

> 上游原版: [OmitNomis/Alvus](https://github.com/OmitNomis/Alvus)
>
> 本 fork 新增: **管理模式**（`--manage`），一个命令启动/管理多个 API 供应商代理；
> **安全加固**（管理端点鉴权）、**Dashboard 分离**、**子进程日志持久化**。

---

## 快速上手

```bash
# 编译（需要 Go 1.22+，因为使用了 //go:embed 等功能）
go build -o alvus.exe .

# 直接运行（已预编译，跳到下一步）
./alvus.exe -local

# 管理模式启动多供应商
./alvus.exe -manage manage.json
```

---

## 单实例模式（跟原版一样）

用法跟原版完全一致，没有任何行为变化。

### 配置 `.env`

```env
PORT=4000
TARGET_BASE_URL=https://integrate.api.nvidia.com/v1
API_KEYS=key1,key2,key3
COOLDOWN_SEC=60
ADMIN_TOKEN=your-secret-token    # 可选，设置后管理 API 需要 X-Admin-Token 头
```

> **安全提示**: 设置 `ADMIN_TOKEN` 后，`POST /api/config` 端点需要提供 `X-Admin-Token` 请求头才能修改配置，防止未经授权的配置篡改。留空则向后兼容（无需鉴权）。

### 启动

```bash
./alvus.exe -local
./alvus.exe -network-only
```

### 检查

浏览器打开 `http://localhost:4000/dashboard`。

---

## 管理模式（本 fork 新增）

### 原理

```
你执行:
  ./alvus.exe --manage manage.json

发生了什么:
  ┌─ alvus.exe (管理器) ────────────────────────┐
  │                                              │
  │  ├─ 启动子进程: NVIDIA (端口 4000)           │
  │  │   └─ 自动创建工作目录 + .env              │
  │  │   └─ key 轮换 → 上游 API                 │
  │  │                                            │
  │  ├─ 启动子进程: SenseNova (端口 4002)        │
  │  │   └─ 自动创建工作目录 + .env              │
  │  │   └─ key 轮换 → 上游 API                 │
  │  │                                            │
  │  └─ 每个子进程独立互不干扰                    │
  │                                                │
  │  挂了自动重启 ✓  Ctrl+C 全关 ✓               │
  └────────────────────────────────────────────────┘
```

**所有配置只写 `manage.json` 一个文件**，不需要建文件夹，不需要写 `.env`。

### 配置 manage.json

```json
{
  "providers": [
    {
      "name": "nvidia",
      "target_url": "https://integrate.api.nvidia.com/v1",
      "genai_url": "https://ai.api.nvidia.com",
      "api_keys": ["nvapi-key1", "nvapi-key2"],
      "port": 4000
    },
    {
      "name": "sensenova",
      "target_url": "https://token.sensenova.cn/v1",
      "api_keys": ["sk-key1"],
      "port": 4002
    }
  ]
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | ✅ | 供应商名称 |
| `target_url` | ✅ | 上游 API 地址 |
| `genai_url` | ❌ | NVIDIA GenAI 专用地址 |
| `api_keys` | ✅ | API key 列表，至少一个 |
| `port` | ❌ | 监听端口，不填自动分配（从 4000 开始） |
| `disabled` | ❌ | `true` 时跳过此供应商 |

### 启动

```bash
./alvus.exe -manage manage.json
```

（`--manage` 也可以，Go flag 包两种格式都支持。）

### 停止

按 `Ctrl+C`，管理器会关掉所有子进程，自动清理临时文件。

### 日志

管理模式会自动在 `logs/alvus-manage.log` 记录完整日志（同时输出到终端）：
- 每个子进程的 stdout/stderr 实时写入
- 子进程退出码记录
- 自动重启事件记录

### 自动重启

子进程意外挂了，管理器每 3 秒检查一次，自动拉起来。

---

## 如何新增一个供应商

**只需要一步：在 manage.json 加一段花括号。**

加一个 DeepSeek 的示例：

```json
{
  "providers": [
    { "name": "nvidia",   "target_url": "https://integrate.api.nvidia.com/v1",   "api_keys": ["key1"], "port": 4000 },
    { "name": "sensenova", "target_url": "https://token.sensenova.cn/v1",        "api_keys": ["sk-1"],  "port": 4002 },
    { "name": "deepseek",  "target_url": "https://api.deepseek.com/v1",          "api_keys": ["sk-d1"], "port": 4003 }
  ]
}
```

然后重启管理器就行。**不需要建文件夹，不需要写 .env 文件，端口可以自动分配。**

---

## 常见场景

### 端口不填会怎样

自动分配：第一个 4000，第二个 4001，以此类推。

### 想暂时关掉某个供应商

```json
{ "name": "openai", "target_url": "...", "api_keys": [...], "port": 4001, "disabled": true }
```

加 `"disabled": true`，下次启动会跳过。

### manage.json 安全吗

`manage.json` 已加入 `.gitignore`，不会误提交到 git。

---

## 目录结构说明

```
alvus-fork/
├── main.go                 # 单实例逻辑（跟原版一样）
├── manage.go               # 管理模式（本 fork 新增）
├── dashboard.html          # Dashboard 页面（通过 //go:embed 嵌入）
├── Dockerfile              # Docker 多阶段构建
├── .dockerignore           # Docker 构建上下文排除
├── docker-compose.yml      # Docker Compose 一键部署
├── alvus.exe               # 编译好的二进制
├── go.mod
├── README.md               # 本说明
├── .env.example            # 环境变量模板
├── manage.json             # 你的配置（已 gitignore，不会误提交）
├── manage.example.json     # 配置模板
├── regression_test.ps1     # 回归测试（22 用例）
├── lint.ps1                # 本地 lint 脚本（go vet + staticcheck）
├── logs/                   # 运行时日志（gitignore）
│   └── alvus-manage.log    #   管理模式日志文件
├── docs/                   # 研究 / 分析文档（gitignore）
├── manage-work/            # 自动生成，程序启动后自动创建（gitignore）
└── .github/workflows/
    └── release.yml         # CI 自动构建发布
```

---

## Docker 部署

### 前置条件

- 安装 [Docker](https://docs.docker.com/get-docker/) 和 [Docker Compose](https://docs.docker.com/compose/install/) (v2.0+)
- 从 `.env.example` 创建 `.env` 文件并填入 API keys 等配置

### 快速启动

```bash
# 1. 创建 .env 配置文件（根据实际情况修改 API keys）
cp .env.example .env

# 2. 构建并启动容器
docker compose up -d

# 3. 检查健康状态
curl http://localhost:3000/health

# 4. 查看日志
docker compose logs -f
```

### 配置说明

- **环境变量**: 通过 `docker-compose.yml` 同级目录下的 `.env` 文件配置（与裸运行相同）
- **端口映射**: 默认映射 `3000:3000`，可通过 `PORT` 环境变量覆盖
- **日志**: 默认输出到 stdout，通过 `docker compose logs` 查看

```bash
# 自定义端口启动（例如映射到 8080）
PORT=8080 docker compose up -d
# 访问 http://localhost:8080/health

# 查看实时日志
docker compose logs -f

# 停止容器
docker compose down
```

### 仅构建

```bash
docker build -t alvus .
docker run --rm alvus --help
```

### Docker HEALTHCHECK

容器内置健康检查（每 30 秒探测 `/health` 端点），不健康时根据 `restart: unless-stopped` 策略自动重启。可通过以下命令查看健康状态：

```bash
docker inspect --format='{{json .State.Health}}' alvus-alvus-1
```

---

## 回归测试

不需要 API key，在本地就能跑：

```powershell
.\regression_test.ps1
```

测试内容：单实例模式（启动、健康检查、配置读写、Dashboard、Key 掩码）、管理模式（多实例启动、非法配置处理）、进程管理（自动重启、停止传播）。

---

> 有问题开 Issue。

---

## 负载压测 & 基准测试

本项目包含两种性能测试方式：

### Go Benchmark（单元级基准测试）

测试核心路径的性能：

```powershell
# KeyPool.Next() 基准测试（1/5/10 个 Key）
go test -bench=BenchmarkKeyPoolNext -benchmem ./internal/keypool/

# Proxy Handler 基准测试（正常 / 全冷却 / 抖动）
go test -bench=BenchmarkProxy -benchmem .
```

### Vegeta 负载压测（HTTP 级）

使用 [Vegeta](https://github.com/tsenart/vegeta) 进行 HTTP 负载压测，覆盖三个场景：
- **正常并发** — 500 QPS, 60s
- **全 Key 冷却** — 所有 Key 被限流时
- **上游 429 抖动** — 上游不稳定时

详见 [`test/load/README.md`](test/load/README.md)。
