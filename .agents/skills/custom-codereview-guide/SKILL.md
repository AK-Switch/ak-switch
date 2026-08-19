---
name: custom-codereview-guide
description: 中文代码审查指南。补充内置 code-review skill，强制中文输出，提供多角度审查方法论、并行子 agent 拆解、置信度评分。
triggers:
  - /codereview
  - /codereview-roasted
---

## 语言要求（最高优先级）

所有输出（评论、报告、结论、review body、行内评论）**必须使用中文**。如果内置 skill 的模板是英文，忽略其语言，用中文重写。

## 优先级声明（强制）

本 skill（custom-codereview-guide）是此仓库的**权威审查框架**。如果其他同时注入的 code-review skill 的内容与本 skill 存在冲突，以本 skill 的指令为准。请严格按照本 skill 定义的 8 步流程执行，忽略其他 code-review skill 中冲突的格式、输出风格和审查方法论。

对给定的 pull request 进行代码审查。

为此，请严格遵循以下步骤：

1. 使用轻量 agent 检查这个 pull request (a) 是否已关闭，(b) 是否为 draft，(c) 是否不需要代码审查（例如因为是自动化 pull request，或改动极小且明显正确），或 (d) 你是否已经对其进行过代码审查。如果是，请勿继续。

2. 使用另一个轻量 agent 给你一份代码库中任何相关 AGENTS.md 文件的文件路径列表（而非内容）：根目录的 AGENTS.md 文件（如果存在），以及被 pull request 修改的目录中的 AGENTS.md 文件。

3. 使用轻量 agent 查看 pull request，并要求 agent 返回改动摘要。

4. 然后，启动 5 个并行 agent 独立审查改动。这些 agent 应执行以下操作，然后返回问题列表及每个问题被标记的原因（例如：AGENTS.md 合规、bug、历史 git 上下文等）：
   a. Agent #1：审计改动，确保其符合 AGENTS.md。注意：AGENTS.md 是指导 AI 编写代码的规范，并非所有条款都适用于代码审查。
   b. Agent #2：读取 pull request 中的文件改动，然后对明显的 bug 进行浅层扫描。避免读取改动范围之外的额外上下文，只关注改动本身。专注于大型 bug，忽略小问题和吹毛求疵。忽略明显的误报。
   c. Agent #3：读取被修改代码的 git blame 和历史，结合历史上下文识别 bug。
   d. Agent #4：读取之前涉及这些文件的 pull request，检查是否有评论也可能适用于当前的 pull request。
   e. Agent #5：读取修改文件中的代码注释，确保 pull request 的改动符合注释中提供的任何指导。

5. 对于步骤 #4 中发现的每个问题，启动并行轻量 agent，该 agent 接收 PR、问题描述和 AGENTS.md 文件列表（来自步骤 #2），并返回一个分数，以表示该 agent 对问题是真实问题还是误报的置信度。该 agent 应按 0-100 的分数评分，以表示其置信度。如果问题因 AGENTS.md 条款而被标记，agent 应 double check 确认 AGENTS.md 是否确实明确指出了该问题。评分标准为（将以下评分标准原样传达给 agent）：
   a. 0：完全不自信。这是一个误报，经不起简单推敲，或是 pre-existing issue。
   b. 25： somewhat confident。这可能是真实问题，但也可能是误报。Agent 无法验证这是否是真实问题。如果问题是风格问题，那它不是相关 AGENTS.md 中明确指出的那种。
   c. 50： moderately confident。Agent 已验证这是真实问题，但可能是 nitpick 或不常发生。相对于 PR 的其他部分，它不是很重要。
   d. 75： highly confident。Agent 双重检查后确认这很可能是真实问题，会在实践中遇到。现有方法不足。问题很重要，会直接影响代码功能，或直接在相关 AGENTS.md 中被提到。
   e. 100： absolutely certain。Agent 双重检查后确认这 definitely 是真实问题，会在实践中频繁发生。证据直接确认。

6. 过滤掉所有分数低于 80 的问题。如果没有满足条件的问题，请勿继续。

7. 使用轻量 agent 重复 #1 的资格检查，确保 pull request 仍然符合代码审查条件。

8. 最后，使用 gh 命令在 pull request 上发表评论。撰写评论时请记住：
   a. 保持输出简洁
   b. 避免使用 emoji
   c. 引用和链接相关代码、文件和 URL

   **发布 review 前必须验证行号（步骤 8 的强制要求）**：
   d. 在发布 inline review comment 之前，对每个 `line` 字段，必须用 `gh pr diff --repo {repo} {pr-number}` 获取 diff 内容，并确认该行号在 diff hunk（`@@` 块）的范围内。如果行号不在 diff 范围内，则 **不要** 将它作为 inline comment 发布，而是将其移入 review body 的文本描述中。
   
   **422 回退策略（步骤 8 的强制要求）**：
   e. 如果 `gh api -X POST` 返回 422 错误（"Unprocessable Entity"），说明某些行号与 diff 不匹配。此时：
      - 将所有 inline comments 转换为 review body 中的纯文本描述（格式：`文件路径#L行号: 问题描述`）
      - 重新发布，使用 `"comments": []`（空的 comments 数组）
      - 如果 422 仍然发生，则直接只发布 review body，不带任何 inline comments

误报示例（步骤 #4 和 #5）：

- Pre-existing issues
- 看起来像 bug 但实际上不是 bug 的情况
- 高级工程师不会专门指出的吹毛求疵的 nitpicks
- Linter、类型检查器、编译器能 catches 的问题（例如：缺失或不正确的导入、类型错误、损坏的测试、格式问题、像换行这样的琐碎样式问题）。你不需要自己运行这些构建步骤——假设它们会作为 CI 的一部分单独运行，这样是安全的。
- 通用代码质量问题（例如：测试覆盖不足、一般安全问题、文档差），除非 AGENTS.md 明确要求
- AGENTS.md 中提到的问题，但在代码中被明确禁用了（例如由于 lint ignore 注释）
- 可能是故意为之的改动，或与整体改动直接相关的改动
- 真实问题，但位于用户未在 pull request 中修改的行

注意事项：

- 不要检查构建信号或尝试构建/类型检查应用程序。这些将单独运行，与你的代码审查无关。
- 使用 `gh` 与 GitHub 交互（例如：获取 pull request、创建行内评论），而不是 web fetch
- 先做 todo list
- 你必须引用和链接每个 bug（例如：如果引用 AGENTS.md，你必须链接它）
- 对于你的最终评论，请严格遵循以下格式（假设你发现了 3 个问题）：

---

### Code review

Found 3 issues:

1. <brief description of bug>（AGENTS.md says "<...>"）

<link to file and line with full sha1 + line range for context, note that you MUST provide the full sha and not use bash here, eg. https://github.com/anthropics/claude-code/blob/1d54823877c4de72b2316a64032a54afc404e619/README.md#L13-L17>

2. <brief description of bug>（some/other/AGENTS.md says "<...>"）

<link to file and line with full sha1 + line range for context>

3. <brief description of bug>（bug due to <file and code snippet>）

<link to file and line with full sha1 + line range for context>

---

- 或者，如果你没有发现任何问题：

---

### Code review

No issues found. Checked for bugs and AGENTS.md compliance.

---

- 链接到代码时，请严格遵循以下格式，否则 Markdown 预览将无法正确渲染：https://github.com/anthropics/claude-cli-internal/blob/c21d3c10bc8e898b7ac1a2d745bdc9bc4e423afe/package.json#L10-L15
  - 需要完整 git sha
  - 你必须提供完整的 sha。像 `https://github.com/owner/repo/blob/$(git rev-parse HEAD)/foo/bar` 这样的命令是行不通的，因为你的评论将直接在 Markdown 中渲染。
  - Repo 名称必须与你审查的 repo 匹配
  - 文件名后必须有 `#` 符号
  - 行号范围格式为 `L[start]-L[结束]`
  - 提供至少 1 行上下文（例如：如果你评论第 5-6 行，链接应为 `L4-7`）
