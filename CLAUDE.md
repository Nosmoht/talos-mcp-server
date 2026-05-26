# Claude Code Memory

@AGENTS.md

This file is intentionally small. `AGENTS.md` is the canonical repository
policy for all coding agents and is imported above with Claude Code's `@`
syntax.

## Claude-Specific Instructions

- Follow `AGENTS.md` for coding conventions, worktrees, reviews, commits,
  issue workflow, safety rules, and testing.
- Do not duplicate policy from `AGENTS.md` here. Update `AGENTS.md` when a
  rule should apply to Claude Code, Codex, Copilot, Cursor, and other agents.
- Keep human/operator documentation in `README.md` or purpose-built docs.
- Keep transient findings, implementation plans, and command output out of
  Claude memory.

## Claude Review Artifacts

- Reviewer agents live in `.claude/agents/`.
- Review artifacts are local-only under `.claude/reviews/<change-id>/`.
- Plan artifacts, when used, are local-only under `.claude/plans/`.
- The permanent audit trail is the commit tag `[review:<change-id>]`.

## Claude Memory Hygiene

- Use `/memory` only for durable project instructions.
- Do not store secrets, one-off findings, scratch TODOs, local machine state,
  or personal preferences.
