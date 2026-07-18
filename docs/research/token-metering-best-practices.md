# Token Metering Best Practices for LLM API Gateways

> 调研日期：2026-07-18
> 调研范围：主流 LLM API 网关 / 可观测性平台的 token 计量方案

---

## 目录

1. [主流 LLM API 网关的 token 计量方式](#1-主流-llm-api-网关的-token-计量方式)
2. [准确性方法对比](#2-准确性方法对比)
3. [流式响应 (SSE) 下的 token 计量](#3-流式响应-sse-下的-token-计量)
4. [提供商差异与统一处理](#4-提供商差异与统一处理)
5. [Tiktoken 的局限与替代方案](#5-tiktoken-的局限与替代方案)
6. [校准机制](#6-校准机制)
7. [总结：对 AK-Switch 的建议](#7-总结对-ak-switch-的建议)

---

## 1. 主流 LLM API 网关的 token 计量方式

### 1.1 Helicone

**来源：** [Helicone GitHub](https://github.com/Helicone/helicone) | [Helicone Docs](https://docs.helicone.ai/)

Helicone 是一个开源 LLM 可观测性平台，提供 AI Gateway 和日志分析功能。

**Token 计量方式：**

- Helicone 作为反向代理，在转发请求时记录完整的请求/响应数据，包括 token 用量
- 非流式请求：从响应的 `usage` 字段直接提取（解析 OpenAI 和 Anthropic 格式）
- 流式请求：通过代理层记录 -- 使用 Cloudflare Workers 拦截流式响应，在流结束时从最后一个 chunk 获取 token 信息
- 支持通过 `Helicone-RateLimit-Policy` 头实现基于 token 的速率限制（文档中标注为 "Coming Soon"）
- 成本追踪：基于预定义的模型定价数据库（300+ 模型）将 token 数转换为成本

**关键点：** Helicone 优先使用上游 API 返回的实际 token 数，而非估算值。它维护一个 LLM 定价数据库用于成本计算。

### 1.2 OpenRouter

**来源：** [OpenRouter Streaming Docs](https://openrouter.ai/docs/api_reference/streaming.md) | [OpenRouter Parameters](https://openrouter.ai/docs/api_reference/parameters.md)

OpenRouter 是一个统一 API 代理，提供对 400+ 模型的访问。

**Token 计量方式：**

- **非流式响应：** 响应体包含 `usage` 字段，含 `prompt_tokens` 和 `completion_tokens`（统一格式，无论后端 provider 是谁）
- **流式响应：** 最后一个 chunk 包含 `usage` 信息。OpenRouter 的文档明确说明：
  ```
  // Final chunk includes usage stats
  if (chunk.usage) {
    console.log('Usage:', chunk.usage);
  }
  ```
- OpenRouter 负责将后端 provider 的 token 格式统一转换为 OpenAI 兼容的 `usage` 对象
- 负责计费：OpenRouter 基于实际 token 用量向用户收费，因此它必须准确获取 token 数

**关键点：** OpenRouter 利用上游 provider 在最后一个 SSE chunk 中返回的 `usage` 字段，不做估算。这是最准确的方案。

### 1.3 Portkey

**来源：** [Portkey Gateway GitHub](https://github.com/Portkey-AI/gateway) | [Portkey Docs](https://portkey.ai/docs/introduction/what-is-portkey)

Portkey 是一个 AI Gateway，提供路由、重试、回退和可观测性。

**Token 计量方式：**

- 作为反向代理，Portkey 从上游响应中提取 token 用量
- 提供 `Get tokens data` API 端点用于查询聚合 token 数据
- 支持成本追踪，基于预定义的模型定价数据库
- Portkey 的 Gateway 是开源的，但完整的 token 计量和成本分析功能在托管版本中更完善

**关键点：** Portkey 的 token 计量依赖于上游 API 返回的实际值，不做 tiktoken 估算。

### 1.4 Kong AI Gateway

**来源：** Kong 官方文档（基于公开信息）

Kong AI Gateway 是 Kong 的 API 管理平台扩展，专门用于 AI/LLM API。

**Token 计量方式：**

- 通过 `ai-proxy` 插件代理 LLM 请求
- 支持基于 token 的速率限制（token-based rate limiting）
- 从上游响应中提取 token 用量
- 与 Kong 的现有观测体系集成（Prometheus 指标、日志）

**关键点：** Kong 的 token 计量能力主要面向速率限制和配额管理，而非精确的用量记录。

### 1.5 Azure API Management

**来源：** 官方文档（基于公开信息）

Azure API Management 通过 `azure-openai-emit-token-metric` 策略支持 token 计量。

**Token 计量方式：**

- 提供 `azure-openai-emit-token-metric` 策略，从 Azure OpenAI 响应中提取 `usage.prompt_tokens` 和 `usage.completion_tokens`
- 将 token 用量导出为自定义指标，可配置告警
- 支持基于 token 的配额限制

**关键点：** Azure APIM 的策略基于 Azure OpenAI 的响应格式，不适用于其他 provider。

### 1.6 Langfuse

**来源：** [Langfuse Token & Cost Tracking Docs](https://langfuse.com/docs/observability/features/token-and-cost-tracking)

Langfuse 是一个开源 LLM 可观测性平台，提供 token 和成本追踪。

**Token 计量方式（双模式）：**

**模式 A：Ingest（摄取）** -- 优先方案
- 从 LLM 提供商的响应中提取实际 token 数
- 支持通过 SDK 直接传入 `usage_details` 字典
- 支持 OpenAI 格式的 `usage` schema（`prompt_tokens`/`completion_tokens`），自动映射
- 支持 Anthropic 的 `response.usage.input_tokens`/`output_tokens`

**模式 B：Infer（推断）** -- 后备方案
- 如果没有摄取 token 数据，Langfuse 使用内置 tokenizer 估算
- 支持的 tokenizer:
  - OpenAI: `o200k_base` (gpt-4o) / `cl100k_base` (gpt-*) via `tiktoken`
  - Anthropic: `@anthropic-ai/tokenizer` 包
  - 其他：用户可自定义模型定义
- 成本推断基于模型定义中的单价

**关键设计：** Langfuse 的 `usage_details` 使用互斥桶（mutually exclusive buckets）：
- 每个 token 必须只在一个桶中计数
- `input` 必须排除 `input_cached_tokens` 中的 token
- 如果 provider 返回的是"包含式"计数（如 OpenAI 的 `prompt_tokens` 包含 cached_tokens），Langfuse 会在摄入时自动做减法

**对于 AK-Switch 的参考价值：** 最高之一。Langfuse 的"先摄取实际值，回退到估算值"策略与 AK-Switch 的思路一致，且其互斥桶设计值得借鉴。

### 1.7 LiteLLM

**来源：** [LiteLLM Token Counter](https://github.com/BerriAI/litellm/blob/main/litellm/litellm_core_utils/token_counter.py) | [LiteLLM Callbacks](https://docs.litellm.ai/docs/observability/custom_callback)

LiteLLM 是一个轻量级的 LLM API 代理，提供统一接口。

**Token 计量方式：**

- 使用 `tiktoken` 作为主要 token 计数器
- 模型映射：
  - `gpt-4o` 系列 -> `o200k_base`
  - 其他 OpenAI 模型 -> `cl100k_base`
  - 未知模型 -> `cl100k_base`（fallback）
- 支持 HuggingFace tokenizer 作为自定义 tokenizer
- 支持完整的消息 token 计数（含 system message、tool calls、function calls、images）
- 从 `kwargs` 中提取 `response_cost`（由 LiteLLM 内部计算）
- 为代理模式提供 `async_log_success_event` 钩子，可在其中获取 token 和成本信息

**关键点：** LiteLLM 的 token 计数策略是"尽力估算"，而非依赖上游返回值。它的 token_counter.py 包含 800+ 行，处理了非常多的边界情况。

### 1.8 LangSmith

**来源：** [LangSmith Observability](https://docs.langchain.com/langsmith/observability.md)

LangSmith 是 LangChain 的 LLM 可观测性平台。

**Token 计量方式：**

- 通过框架集成（LangChain、OpenAI SDK 等）自动捕获 token 用量
- 与 Langfuse 类似，支持从上游响应中提取实际 token 数
- 当与实际值配合使用时，LangSmith 的追踪功能会自动记录 token 用量


---

## 2. 准确性方法对比

### 方法 A：从响应体 JSON 解析 usage 字段（最准确）

| 提供商 | 字段路径 | 示例 |
|--------|---------|------|
| OpenAI | `usage.prompt_tokens`, `usage.completion_tokens` | `{"usage": {"prompt_tokens": 10, "completion_tokens": 25}}` |
| Anthropic | `usage.input_tokens`, `usage.output_tokens` | `{"usage": {"input_tokens": 10, "output_tokens": 25}}` |
| Google Gemini | `usageMetadata.promptTokenCount`, `usageMetadata.candidatesTokenCount` | `{"usageMetadata": {"promptTokenCount": 10}}` |

**优点：**
- 100% 准确，与计费系统一致
- 包含 prompt caching、reasoning tokens 等复杂场景的正确计数

**缺点：**
- 非流式响应需要等待完整响应体
- 流式响应中，usage 数据只在最后一个 chunk 中
- 格式因 provider 而异

**适用场景：** 所有非流式请求，以及流式请求的最终计数。

### 方法 B：Tiktoken 估算（快速但近似）

**来源：** [OpenAI tiktoken](https://github.com/openai/tiktoken)

**优点：**
- 无需等待完整响应，可在流式传输过程中实时估算
- 不依赖上游 API 的响应格式

**缺点：**
- 与 API 实际计费存在偏差（通常 5-15%）
- 需要选择正确的 tokenizer 编码
- Anthropic 的 tokenizer 与 tiktoken 不兼容
- 无法处理视觉、音频等多模态输入的 token 计数
- 无法处理 prompt caching 的 token 减免

**适用场景：** 流式响应的实时估算、不支持 usage 字段的模型。

### 方法 C：调用 provider 的 usage API

有些 provider 提供专门的 usage 查询端点，但这不是通用做法。

- OpenAI 没有单独的 usage 端点
- Anthropic 也没有单独的 usage 端点
- 大多数 provider 只在响应中返回 usage 信息

**结论：** 这不是一个可行的通用方案。

### 方法 D：Langfuse 的"先摄取，后推断"策略

**来源：** [Langfuse Token & Cost Tracking](https://langfuse.com/docs/observability/features/token-and-cost-tracking)

Langfuse 的方案是当前最成熟的实践：

```
Ingested Observation
  +-> Includes usage? -> YES -> 使用实际值（最准确）
  +-> Includes usage? -> NO  -> 使用 tokenizer 估算（后备）
      +-> 如果 cost 也缺失 -> 使用模型单价 x usage 推断
```

**关键 insight：** 这种分层策略可以保证：
1. 非流式响应：使用上游返回的实际值（100% 准确）
2. 流式响应：使用 tiktoken 估算（有误差，但比没有好）
3. 如果有后续的非流式请求，可以用实际值校准估算


---

## 3. 流式响应 (SSE) 下的 token 计量

### 3.1 OpenAI 流式 token 信息

OpenAI 的流式 SSE 响应在**最后一个 chunk** 中包含 `usage` 字段：

```
data: {"id":"...", "object":"chat.completion.chunk", ...,
       "usage": null,
       "choices":[...]}

// ... 中间 chunk ...

data: {"id":"...", "object":"chat.completion.chunk", ...,
       "usage": {"prompt_tokens": 10, "completion_tokens": 25, "total_tokens": 35},
       "choices":[{"delta":{},"finish_reason":"stop"}]}
```

### 3.2 Anthropic 流式 token 信息

Anthropic 的流式消息使用 `message_start` 和 `message_delta` 事件：

- `message_start` 事件包含 `message.usage`（含 `input_tokens`）
- `message_delta` 事件包含 `usage`（含 `output_tokens`）
- 在流结束时，`message_delta` 中的 `output_tokens` 是最终值

### 3.3 OpenRouter 的流式处理

**来源：** [OpenRouter Streaming Docs](https://openrouter.ai/docs/api_reference/streaming.md)

OpenRouter 在流式响应中，最后一个 chunk 包含 `usage` 信息：

```typescript
// Final chunk includes usage stats
if (chunk.usage) {
  console.log('Usage:', chunk.usage);
}
```

OpenRouter 负责将后端 provider 的 token 格式统一转换为 OpenAI 兼容格式。

### 3.4 流式 token 计量的行业方案

| 方案 | 描述 | 代表项目 |
|------|------|---------|
| **等待最后一个 chunk** | 流传输过程中不计数，等待最后一个 chunk 中的 usage 字段 | OpenRouter、Helicone |
| **tiktoken 实时估算** | 在流式传输过程中用 tiktoken 估算输出 token | AK-Switch（当前方案）、LiteLLM |
| **混合方案** | 流式用 tiktoken 估算，非流式用实际值，再用实际值校准估算 | AK-Switch（校准器）、Langfuse |

**关键问题：** 如果代理在转发时不知道总 token 数，常用的解决方案是什么？

**答案：** 业界最常用的方案是"流结束时获取最后一个 chunk 中的 usage 字段"。这意味着：
- 代理需要先转发所有数据，然后在流结束时记录 token 数
- 对于需要实时计费的场景（如速率限制），可以在流结束后再执行扣费
- 如果无法获取最后一个 chunk（如代理与客户端断开），则回退到 tiktoken 估算

### 3.5 AK-Switch 当前方案的问题

AK-Switch 当前在 `streamSSEAndEstimateTokens` 中：
1. 转发 SSE 事件到客户端
2. 只解析 `content_block_delta` 事件（Anthropic 格式）来累加文本
3. 流结束后用 tiktoken 估算

**问题：**
- 没有解析最后一个 chunk 中的 `usage` 字段（OpenAI 和 OpenRouter 的用法）
- 没有解析 `message_delta` 中的 `output_tokens`（Anthropic 的用法）
- 只用了 `cl100k_base` 编码，对 gpt-4o 系列（需 `o200k_base`）不准
- 对 Anthropic 模型用 tiktoken 估算会出现系统偏差


---

## 4. 提供商差异与统一处理

### 4.1 各 provider 的 token 字段对照

| 提供商 | 输入 token 字段 | 输出 token 字段 | 缓存 token | 推理 token |
|--------|----------------|-----------------|-----------|-----------|
| OpenAI | `usage.prompt_tokens` | `usage.completion_tokens` | `usage.prompt_tokens_details.cached_tokens` | `usage.completion_tokens_details.reasoning_tokens` |
| Anthropic | `usage.input_tokens` | `usage.output_tokens` | `usage.cache_read_input_tokens`, `usage.cache_creation_input_tokens` | `usage.output_tokens_details` |
| Google Gemini | `usageMetadata.promptTokenCount` | `usageMetadata.candidatesTokenCount` | `usageMetadata.cachedContentTokenCount` | -- |
| OpenRouter | `usage.prompt_tokens` (统一为 OpenAI 格式) | `usage.completion_tokens` | -- | -- |

### 4.2 统一处理策略

**方案 1：适配器模式**（推荐）

为每个 provider 实现一个 TokenExtractor 接口：

```go
type TokenExtractor interface {
    ExtractInputTokens(body []byte) int
    ExtractOutputTokens(body []byte) int
    ExtractStreamingUsage(events []SSEEvent) (inputTokens, outputTokens int)
}
```

各 provider 实现：
- `openaiTokenExtractor` -- 解析 `usage.prompt_tokens` / `usage.completion_tokens`
- `anthropicTokenExtractor` -- 解析 `usage.input_tokens` / `usage.output_tokens`
- `geminiTokenExtractor` -- 解析 `usageMetadata.promptTokenCount` / `usageMetadata.candidatesTokenCount`

**方案 2：统一字段映射**（中间格式）

AK-Switch 内部使用统一的 `input_tokens` / `output_tokens` 字段，在提取时直接映射：

```go
// 尝试多种格式，第一个成功的结果
func extractTokenUsage(body []byte, provider string) (inputTokens, outputTokens int) {
    switch provider {
    case "openai":
        return extractOpenAI(body)
    case "anthropic":
        return extractAnthropic(body)
    case "gemini":
        return extractGemini(body)
    default:
        if t, o := extractAnthropic(body); t > 0 { return t, o }
        if t, o := extractOpenAI(body); t > 0 { return t, o }
        return 0, 0
    }
}
```

**方案 3：Langfuse 的互斥桶模型**

**来源：** [Langfuse Usage Details Contract](https://langfuse.com/docs/observability/features/token-and-cost-tracking#usage-details-contract)

Langfuse 使用 `usage_details` 字典，key 为任意字符串，但要求互斥：
- 每个 token 必须只在一个桶中计数
- `input` 必须排除 `input_cached_tokens` 中的 token
- 如果 provider 返回的是"包含式"计数（如 OpenAI 的 `prompt_tokens` 包含 cached_tokens），需要做减法

这是最灵活的设计，但实现较复杂。

### 4.3 AK-Switch 当前做法

当前 `extractTokenUsage` 只解析 Anthropic 格式（`usage.input_tokens` / `usage.output_tokens`），不处理 OpenAI 格式。这是明确的缺口。


---

## 5. Tiktoken 的局限与替代方案

### 5.1 tiktoken 编码与模型对应关系

**来源：** [OpenAI tiktoken](https://github.com/openai/tiktoken)

| 编码 | 适用模型 | 说明 |
|------|---------|------|
| `o200k_base` | gpt-4o, gpt-4o-mini, o1, o3, gpt-5+ | 最新编码，更大的词汇表 |
| `cl100k_base` | gpt-4, gpt-3.5-turbo, text-embedding-ada-002 | 最广泛使用的编码 |
| `p50k_base` | text-davinci-003, code-davinci-002 | 代码模型 |
| `r50k_base` | text-davinci-001/002, GPT-3 系列 | 最旧的编码 |

**LiteLLM 的编码选择策略：**

```python
# 来源：litellm/litellm_core_utils/token_counter.py
if "gpt-4o" in model_to_use:
    encoding = tiktoken.get_encoding("o200k_base")
else:
    encoding = tiktoken.encoding_for_model(model_to_use)
# 如果 encoding_for_model 失败，回退到 cl100k_base
```

### 5.2 Anthropic 的 tokenizer

Anthropic 使用独立的 tokenizer，与 OpenAI 的 BPE 不同。Anthropic 提供了 `@anthropic-ai/tokenizer` 包（npm）和 `anthropic[tokenizer]`（Python）。

**关键差异：** 用 `cl100k_base` 估算 Anthropic 的 token 数会产生偏差。Langfuse 文档明确指出：
> "According to Anthropic, their tokenizer is not accurate for Claude 3 models. If possible, send us the tokens from their API response."

这意味着即使是 Anthropic 自己的 tokenizer，对于 Claude 3+ 也可能不够准确，**最准确的方式仍然是使用 API 返回的实际值**。

### 5.3 HuggingFace tokenizers 库

LiteLLM 支持将 HuggingFace tokenizer 作为自定义 tokenizer：

```python
tokenizer_json = {
    "type": "huggingface_tokenizer",
    "tokenizer": AutoTokenizer.from_pretrained("model-name")
}
```

这对于使用开源模型（Llama、Mistral 等）的场景很有用，但对于商业 API（OpenAI、Anthropic）不适用。

### 5.4 是否有统一的 tokenizer 方案？

**没有通用的统一 tokenizer 方案。** 原因：
1. 每个模型家族使用不同的 BPE 词汇表
2. Anthropic 和 OpenAI 使用完全不同的 tokenizer 实现
3. Google Gemini 使用 SentencePiece

**实际做法：** 按模型选择正确的 tokenizer，而不是追求统一方案。

**对于 AK-Switch：** 如果假设上游 provider 会在响应中返回 token 数，tiktoken 只需作为流式估算的 fallback，不需要精确到所有模型。


---

## 6. 校准机制

### 6.1 业界的校准做法

**AK-Switch 的当前做法：** 滑动窗口校准器

AK-Switch 已经实现了一个 `Calibrator`，其工作原理：
1. 非流式响应：从上游获得实际 token 数，同时用 tiktoken 估算
2. 计算 `ratio = actual / estimate`，加入滑动窗口（窗口大小 15）
3. 流式响应：用 tiktoken 估算，然后乘以校准系数（窗口内 ratio 的中位数）
4. 少于 3 个样本时返回 1.0（不做校准）

**来源：** `D:\Work\Projects\AK-Switch\internal\tracker\calibration.go`

**Langfuse 的做法：** 模型定义 + 价格表
- 不依赖滑动窗口校准
- 而是基于模型名称选择正确的 tokenizer 和价格表
- 如果用户提供自定义模型定义，可以覆盖默认行为

**LiteLLM 的做法：** 不做校准
- 直接使用 tiktoken 估算值
- 不知道误差，也不做调整

### 6.2 校准方案对比

| 方案 | 适用场景 | 优点 | 缺点 |
|------|---------|------|------|
| 滑动窗口校准（AK-Switch） | 流式响应估算 | 自适应，随时间收敛 | 需要非流式请求提供训练数据 |
| 固定系数（Langfuse） | 成本推断 | 简单，无需训练 | 不随模型更新自适应 |
| 不做校准（LiteLLM） | 估算要求不高 | 零开销 | 精度差 |

### 6.3 对 AK-Switch 校准器的建议

1. **当前校准器的工作方向正确**，但需要处理一些边界情况：
   - 模型切换后，旧模型的数据不应影响新模型（已按 model 隔离，OK）
   - 窗口大小 15 是合理的，但不应硬编码
   - 校准器只应用于输出 token，输入 token 也可以校准（当前只校准了输出）

2. **可以改进的点：**
   - 在流式 SSE 末尾解析实际 usage 字段（如果存在），将其与校准器估算值对比
   - 如果能从最后一个 chunk 获取实际值，直接使用实际值，跳过校准
   - 添加持久化能力，使校准数据跨进程重启保留


---

## 7. 总结：对 AK-Switch 的建议

### 7.1 当前做法的评估

| 维度 | 当前做法 | 评估 |
|------|---------|------|
| 非流式响应 | `extractTokenUsage` 解析 Anthropic 格式 | **不足**：只处理了 Anthropic 格式，没有 OpenAI/Gemini |
| 流式响应 | `streamSSEAndEstimateTokens` + tiktoken | **良好但可改进**：未解析最后一个 SSE chunk 中的 usage |
| 输入 token 估算 | `estimateInputTokens` 只处理 OpenAI 字符串格式 | **不足**：已有 worktree 修复（支持 Anthropic 数组内容），但未合并 |
| 校准器 | 滑动窗口中位数 | **良好**：业界领先的做法，很少有项目实现这个 |
| Tiktoken 编码 | 固定 `cl100k_base` | **不足**：gpt-4o 系列需要 `o200k_base` |

### 7.2 回答：我们当前的做法（tiktoken fallback）是不是最好的方案？

**综合来看，当前做法是合理的，但可以改进为更优的方案。**

业界最佳实践是 **"三层优先级"** 策略：

```
优先级 1：上游 API 返回的实际值
    +-> 非流式：从响应体 JSON 解析 usage
    +-> 流式：从最后一个 SSE chunk 解析 usage
优先级 2：tiktoken 估算 + 校准器修正
    +-> 当上游不返回 token 信息时使用
优先级 3：纯 tiktoken 估算（无校准）
    +-> 当校准器数据不足时使用
```

AK-Switch 当前正确地实现了优先级 1（非流式）和优先级 2/3（流式），但存在以下缺口：

### 7.3 具体改进建议

**建议 1：支持多个 provider 的 token 提取格式**

实现 `extractTokenUsage` 的 provider 感知版本，支持：
- OpenAI: `usage.prompt_tokens` / `usage.completion_tokens`
- Anthropic: `usage.input_tokens` / `usage.output_tokens`
- Gemini: `usageMetadata.promptTokenCount` / `usageMetadata.candidatesTokenCount`

参考：`extractTokenUsage` 当前在第 412-425 行，只处理 Anthropic 格式。

**建议 2：流式 SSE 末尾解析实际 usage**

在 `streamSSEAndEstimateTokens` 中，除了 `content_block_delta` 外，还应：
- 解析 SSE 末尾的 `usage` 字段（OpenAI 格式）
- 解析 `message_delta` 中的 `usage`（Anthropic 格式）
- 如果获取到实际值，优先使用实际值，跳过 tiktoken 估算

**建议 3：根据模型选择 tiktoken 编码**

当前固定使用 `cl100k_base`，应改为：
- `gpt-4o` / `o1` / `o3` 系列 -> `o200k_base`
- 其他 OpenAI 模型 -> `cl100k_base`
- 未知模型 -> `cl100k_base`（fallback）

**建议 4：合并 fix-input-token-estimation worktree 的改进**

该 worktree 中的 `estimateInputTokens` 改进（支持 Anthropic 数组格式的 content）应该合并到主线。

**建议 5：校准器支持输入 token 校准**

当前校准器只对输出 token 应用 `Apply()`，输入 token 的估算也可以被校准。

**建议 6：考虑使用模型名称推断 provider**

从请求体中的 `model` 字段推断 provider，从而选择正确的 token 提取和估算策略：
- `gpt-*` -> OpenAI 格式
- `claude-*` -> Anthropic 格式
- `gemini-*` -> Gemini 格式

### 7.4 优先级排序

| 优先级 | 建议 | 影响 | 工作量 |
|--------|------|------|--------|
| P0 | 合并 fix-input-token-estimation 的 worktree 修复 | 修复 Anthropic 输入 token 估算崩溃 | 小 |
| P0 | 支持 OpenAI 格式的 `extractTokenUsage` | 非流式 OpenAI 请求缺失 token 计量 | 小 |
| P1 | 流式 SSE 末尾解析实际 usage | 流式 token 计量更准确 | 中 |
| P1 | 根据模型选择 tiktoken 编码 | gpt-4o 系列估算更准确 | 小 |
| P2 | 校准器支持输入 token | 输入 token 估算更准确 | 小 |
| P2 | 模型名称到 provider 的映射 | 减少硬编码，更可扩展 | 中 |

### 7.5 核心结论

**AK-Switch 当前的 tiktoken fallback 方案是合理的，但不是最优的。** 业界最佳实践是优先使用上游 API 返回的实际 token 数，仅在无法获取时才回退到 tiktoken 估算。AK-Switch 已经实现了非流式场景的优先级 1（实际值）和流式场景的优先级 2（tiktoken + 校准），但需要：

1. 扩展 `extractTokenUsage` 以支持更多 provider 格式
2. 在流式 SSE 末尾解析实际 usage 字段
3. 根据模型选择正确的 tiktoken 编码

这些改进将使 AK-Switch 的 token 计量从"基本可用"提升到"接近业界最佳实践"的水平。

---

## 参考来源

| 来源 | URL | 内容 |
|------|-----|------|
| OpenAI tiktoken | https://github.com/openai/tiktoken | BPE tokenizer 实现和编码映射 |
| OpenAI CompletionUsage | https://raw.githubusercontent.com/openai/openai-python/main/src/openai/types/completion_usage.py | `usage.prompt_tokens` / `completion_tokens` 定义 |
| Anthropic Usage | https://raw.githubusercontent.com/anthropics/anthropic-sdk-python/main/src/anthropic/types/usage.py | `usage.input_tokens` / `output_tokens` 定义 |
| OpenRouter Streaming | https://openrouter.ai/docs/api_reference/streaming.md | 流式响应中最后一个 chunk 含 usage |
| Helicone | https://github.com/Helicone/helicone | AI Gateway + 可观测性，从响应提取 token |
| Portkey Gateway | https://github.com/Portkey-AI/gateway | AI Gateway，提供 token 数据和成本追踪 API |
| Langfuse Token & Cost | https://langfuse.com/docs/observability/features/token-and-cost-tracking | "先摄取后推断"策略，互斥桶设计，tokenizer 支持列表 |
| LiteLLM Token Counter | https://github.com/BerriAI/litellm/blob/main/litellm/litellm_core_utils/token_counter.py | Tiktoken 使用、模型映射、图像 token 计算 |
| LangSmith Observability | https://docs.langchain.com/langsmith/observability.md | 通过框架集成自动捕获 token 用量 |
