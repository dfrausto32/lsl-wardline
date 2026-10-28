# Kickoff prompt for Claude Code

Paste this as the first message in a Claude Code session opened in the `app` folder.

---

Read `CLAUDE.md` fully, then `specs/UX.md`, `specs/PLATFORMS.md`, `specs/BUILD-ORDER.md`, `specs/DATA-MODEL.md`, `specs/BUSINESS-RULES.md`, `specs/ACCEPTANCE.md`, and `docs/DEV-ENVIRONMENT.md`.

Start at task 1.0 (container dev environment) and work through Phase 1 in order. For each task: write the named test first, implement, run `./scripts/verify.ps1`, commit with the task ID. Look at every screen you build in Playwright screenshots at desktop and iPhone width before calling it done.

Docker Desktop is installed on this machine. Do not use WSL or install Go on the host. If something needs me (real hardware, accounts, decisions), add it to `BLOCKED.md` and continue with the next task that is not blocked. Stop and ask before building anything that is not in the specs.

When Phase 1 is complete, run the lab end-to-end tests, summarize what you verified versus inferred, and stop. Do not start Phase 2.
