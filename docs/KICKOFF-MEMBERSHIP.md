# Kickoff prompt for Claude Code: Phase 6 (membership log)

Paste this as the first message in a Claude Code session opened in the `app` folder, only when Dylan says go.

---

Read `CLAUDE.md`, then `specs/MEMBERSHIP-AND-MODES.md` in full, then `specs/BUSINESS-RULES.md`, `specs/DATA-MODEL.md`, `specs/UX.md`, and `BLOCKED.md`.

Phases 1 and 2 are built and must keep passing unchanged. Start at task 6.0 (decision record) and work through Phase 6 only, in order. For each task: write the named test first, implement, run `./scripts/verify.ps1`, commit with the task ID. Do not modify existing tests to make them pass. Look at every new screen in Playwright screenshots at desktop and iPhone width before calling it done.

Remember: the Edge is a separate repo and is not part of this work; build only the read-only API in task 6.8. Do not start Phase 7 or later. If you need a decision that is not in the spec, add it to `BLOCKED.md` and continue with the next task that is not blocked, or stop and ask if nothing else can proceed.

When Phase 6 is complete, run the full suite and the lab end-to-end tests, summarize what you verified versus what you inferred, and stop.
