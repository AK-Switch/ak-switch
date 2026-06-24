- 一切问题, 请反思如何从根源解决, 而不是抑制错误

- 所有 Plan 文档请放在: \docs\plan\

## 工作流规范

- main 分支受保护，禁止直接推送。所有变更必须通过 feature 分支 → PR → CI 绿 → AI 审查 → 合并的流程
- 本地开发从 main 切出 feature/xxx 分支，开发完推送到 origin（你自己的 fork）
- 合并到 main 后同步到 upstream（OmitNomis/Alvus）保持 fork 不落后

