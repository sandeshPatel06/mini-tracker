# Changelog

All notable changes to **get-Hike** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] — 2026-10-18

### Added

- **Background tracking after app close**: when the desktop app is closed while tracking is
  active, a detached `--background` process of the same binary continues screenshot capture,
  keyboard/click capture, database writes, AI analysis and sync — exactly like the app while
  open. Reopening the app stops the background process and reclaims tracking in-process.
  - PID-file lifecycle (`data_dir/tracker.pid`), duplicate-daemon guard, clean shutdown on
    SIGTERM/SIGINT with stats flush.
  - Reuses `App.startup` / `App.shutdown` / `collect` unchanged (no duplicated tracking logic).
  - Platform helpers: `setsid` detachment on Linux/macOS, `DETACHED_PROCESS` on Windows,
    safe no-op fallback elsewhere.
  - Unit tests for PID helpers + verified smoke test (start, duplicate start rejected,
    SIGTERM cleans up PID file).
- **MIT `LICENSE` file** (Copyright © 2026 REAK INFOTECH LLP) — the README previously mentioned
  MIT without shipping the license text.
- **Developer Menu (desktop app)**: 7 quick taps (≤2 s apart) on the sidebar/mobile logo unlock
  a session-only developer panel with Logs, Diagnostics, Actions, AI, Share and Debug tabs.
  - Logs: live tail of the daily app log, file browser (7-day retention), level filter, search,
    copy, clear, open logs folder.
  - Diagnostics: build/env info, data/DB/log/screenshot sizes, tracker + background-daemon
    status, AI model with masked key, pending-analysis count, sync status, DB table counts and
    a client-side recent-error digest.
  - Actions: collect now, sync now, process pending AI, toggle tracking, stop background
    daemon, reload UI, purge screenshot cache, database backup (Save dialog) and clear-all-data.
  - AI: analyze latest pending log with raw result/error, model discovery list, runtime log
    level switch.
  - Share (4 buttons): copy diagnostics bundle (markdown), create a prefilled GitHub issue,
    save bundle to file (Save dialog), copy log file path.
  - Debug: localStorage viewer, theme switch, feature-flag store, `apiFetch` network monitor and
    app-event monitor (capped ring buffers), seed/clear demo logs, disable dev mode.
  - Secrets are never displayed (API keys masked); destructive actions require confirmation;
    web/server mode is unaffected (desktop-only bindings, guarded by `window.go`).
- Web-mode mouse click reporting: `/api/tracker/input` now accepts `total_clicks` and forwards
  it to the native mouse tracker.

### Changed

- **README**: repositioned as an open-source project with a dedicated
  "Open Source & Free for Personal Use" section (solo/Guest mode, no account or cloud needed,
  local data, fork/modify/self-host freedom), personal-use note in Quickstart, and a license
  section that points to the new `LICENSE` file. Fixed duplicate/invalid model names.

### Removed

- **Mouse-movement tracking** across the entire stack (click tracking retained):
  - backend: `MouseStats.MouseDistance`, squared-distance accumulation, `EV_REL` / `EV_ABS`
    movement handling, `dx/dy` mouse events;
  - frontend: `requestAnimationFrame`-throttled `mousemove` listener and distance flush payload;
  - AI prompts: all "Mouse Distance" scoring rules and telemetry text;
  - `RecordMouseActivity` signature reduced to clicks-only (Wails bindings updated).
  - The `mouse_distance` DB column is kept for schema compatibility and written as `0`.

### Fixed

Security & sessions:

- Removed spoofable session fallbacks: `X-Session-User-ID` header, unsigned
  `mini_session_user_id` cookie, and the 5-minute global "recent OAuth" user cache.
  Sessions now require a signed JWT (cookie or `Authorization: Bearer`).
- OAuth test-login (`?mock=true` / `?test=true`) is now disabled unless explicitly enabled with
  the `ALLOW_MOCK_OAUTH=true` environment variable.
- Unauthenticated ("guest") access to logs, stats, work sessions, org roster/invitations,
  config writes, tracker input/toggle, screenshots and usage endpoints is now restricted to
  **loopback requests only**; remote requests receive `401`.
- `/api/image` now requires a session (or loopback) and only serves files contained inside the
  configured `DataDir` (prefix-check replaced with `filepath.Rel` containment).
- CORS: credential-bearing responses are only allowed for loopback origins; stray
  `X-Session-User-ID` removed from allowed headers.
- Logout clears the session; the unsigned legacy cookie is no longer issued.
- JWT signing key now comes from `JWT_SECRET` (env) or an unpredictable per-process key —
  the hardcoded secret was removed.

Data integrity, analytics & sync:

- `InsertLog` now persists `org_id`, `user_id`, `app_name` and `window_title` (previously
  silently dropped, so pushed telemetry landed under org 1 / user 1 and titles were lost).
- `UpdateAIResult` stores the model's real `productive_score` instead of forcing `100`/`0`;
  the `score == 0 && productive → 100` rewrite was removed from `UpdateLogAnalysis`.
- Pulled analytics now update local rows via the stored `remote_id`
  (`UpdateAIResultByRemoteID`) instead of mismatched local IDs.
- Per-user usage summary no longer aggregates every user's tokens for anonymous callers, and
  org usage no longer includes `org_admin` rows from other organizations.
- Config: `BackendEndpoint` is finally populated on load, and `Save` persists Google/Azure
  OAuth credentials (previously erased on any settings save).
- `/api/logs` returns `[]` instead of `null` for empty result sets.
- Sync engine: pushes now include `total_clicks`, non-2xx responses and malformed responses
  are logged instead of silently dropped, and the desktop collector uses the single-flight
  `TriggerSyncNow` so pending rows cannot be pushed twice concurrently.

Real AI analytics (no fabricated values):

- Gemini now receives **real screenshots, window titles and click counts** in every analysis
  path: server screenshot upload, telemetry push, pending-log batch processing and the
  frontend guest bundle (which loads screenshots via `GetImageBase64` / `/api/image`).
- Batch responses are mapped by their `item_index`; items with no model result stay pending
  for retry instead of receiving entropy-derived fallback scores or `100`/`0` overrides;
  usage metadata is recorded once per batch; batch processing is single-flight; token usage
  from server-side batch analysis is now recorded.
- Frontend guest AI path no longer substitutes invented values (`90`/`20` scores, `0.95`
  confidence, default `'Browsing'` category) — only values returned by the model are applied;
  duplicate bundle submissions are suppressed per session.

Frontend correctness:

- JWT is forwarded to the Wails sync engine on startup/login; logout clears
  `mini_jwt_token`/`mini_auth_org`; the `mini-auth-expired` event is handled and stale users
  are cleared when `/api/auth/me` reports `authenticated: false`.
- Date-range precedence fixed (explicit range > explicit date > page defaults) with a
  stale-response guard, so Timeline picks and background refreshes no longer fight.
- Dashboard "Latest 10 captures" no longer shows the oldest captures in desktop mode;
  empty analysis renders `—` instead of a red `0%`; hourly chart tooltips show capture counts
  and "No captures" instead of fake `0%` bars.
- Timeline "Avg Day Focus" ignores pending logs; page number resets/clamps on date change.
- Settings: invalid model option values fixed, "Next AI Sync In" derives from the real
  configured interval, the quota card no longer presents hardcoded numbers as live limits,
  "Clear All Stored Data" shows a desktop-only notice in web mode, and the Test API counter
  persists.
- Dead `mini_logs_*` offline fallback removed; tracker status prefers Wails over HTTP when
  both are available; web mode reports mouse clicks.

Accessibility & UI/UX audit (earlier in this release cycle):

- Visible `:focus-visible` outlines, modal dialog semantics with focus trap and scroll lock,
  keyboard navigation, skip link, and `prefers-reduced-motion` support.
- WCAG-AA contrast token adjustments; background refresh no longer flashes skeletons;
  load-error states with retry; local-date handling; unified pending-log detection
  (`isPendingLog`); honest Settings save feedback; OAuth popup timeout/cancel handling;
  defensive JSON parsing; clipboard `await`; proper input labels/autocomplete; dead code
  (`App.css`, `MiniTrackerWizard.tsx`, hidden pill) removed.

---

## Notes

- This file was introduced on 2026-10-18; earlier project history is available via
  `git log`.
- The `mouse_distance` database column and `LogEntry.MouseDistance` field remain for schema
  and API compatibility, but tracking no longer collects movement data (always `0`).
