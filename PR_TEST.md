# Test PR for verifying custom code-review skill

This PR tests that the new PR review workflow correctly uses only our `custom-codereview-guide` skill instead of the bundled plugin code-review skill.

## What to verify

1. Review output is in Chinese
2. Review follows our skill's format (8-step process, 5 parallel agents, confidence scoring)
3. NO plugin code-review format (Taste Rating, 10 scenarios, Risk Assessment, etc.)
4. NO 'Plugin skill code-review overrides existing skill' warning in logs
5. Only 'custom-codereview-guide' is triggered by /codereview
