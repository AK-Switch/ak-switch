# 0001 — Repository Migration

The original `AK-Switch/ak-switch` was a fork of `OmitNomis/Alvus` that was later renamed from `Alvus` to `ak-switch`. Due to its fork origin and rename history, it was undetectable in third-party clients (Sourcegraph, GitKraken, etc.). We migrated to a fresh repository in the same organization: renamed the old repo to `ak-switch-archive` and archived it, then mirrored all git history (branches, tags, commits) to a new `AK-Switch/ak-switch` repo.

Alternatives considered:
- **git init from scratch** — loses all commit history and makes the project look brand-new with no trace of prior work.
- **Move to a different org** — adds org management overhead for no benefit; the AK-Switch org owns the project identity.
- **Keep the fork as-is** — the problem persists: third-party clients still can't discover the repo.

We chose mirror migration with the same org + same name because it preserves all history at the lowest disruption cost while fixing the discoverability problem at its root.