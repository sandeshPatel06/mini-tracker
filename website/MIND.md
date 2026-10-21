# MIND.md — project memory for the get-Hike website

Persistent notes of decisions, state and pitfalls. Read this first when resuming work.

## Identity
- Repo `sandeshPatel06/mini-tracker` (public, SSH remote). Repo root is the whole app; this folder is only the marketing site.
- Live URL: `https://sandeshpatel06.github.io/mini-tracker/` (7 pages: index, features, download, get-started, privacy, changelog, 404).
- User language: Hinglish. Brand: emerald-on-dark, "Privacy-first productivity for Linux".

## Locked-in decisions (do not revisit silently)
1. **Dark-default + light remap theming.** Tailwind palette vars re-pointed under `[data-theme="light"]` in `global.css` (slate inverted, `--color-white:#0f1c23`, emerald-400 → `#047857`, `--color-on-brand:#04231a`). Anti-FOUC bootstrap lives in `BaseHead.astro` (localStorage key `gh-theme`, respects `prefers-color-scheme`, updates `meta[name=theme-color]`).
2. **Theme toggle** rendered by cloning `template.theme-toggle-template` into `.theme-slot` spans at end of `Header.astro`; JS toggles `data-theme` on `<html>` and persists it.
3. **Self-hosted fonts** only (Inter Variable + JetBrains Mono Variable) — migration from CDN started when desktop app (`frontend/src/style.css`) adopted the same two fonts.
4. **Static changelog.** A live API changelog (marked + localStorage cache) was built and deployed in `aebcd26`, user disliked it visually → reverted clean in `549a686`. Current static release list is final until user re-asks.
5. **Release CI is manual-only** (`build.yml`, workflow_dispatch). Pages deploy is automatic for `website/**` pushes.
6. **History was rewritten once** (git-filter-repo: purged 13MB desktop binary, `build/` dir, `logo-universal.png`). Full pre-rewrite backup: `/data/data/com.termux/files/usr/tmp/opencode/mini-tracker-pre-rewrite.bundle`. Never reintroduce that data.

## Recent commit trail (main)
- `ca1f581` rewrite housekeeping → `51956ad` restore `build/appicon.png` (wails packaging needs it) → PR #20 website sync → Pages pipeline → `3f39434` responsive fixes → `b3b5814` mobile terminal-wrap → `c006645` theme/fonts/readability → revert → `ba573fe` skill-driven polish (44px targets, SVG icons, lazy logo, copy hit-area).

## Design-system context (from ui-ux-pro-max skill)
- Style: Dark Mode (OLED) acceptance — required `contrast-text-4.5`, keyboard, visible-focus, reduced-motion: all satisfied.
- Patterns decided against: testimonials carousel (no testimonials available), charts (nothing to chart).
- Deliberate exemptions: `text-[10px]`/`[11px]` used only for chips/pills/mock HUD counts (WCAG target-size / label exception), checkmark glyph `✓`/`✗` in content lists are typography, not icons.

## Pitfalls learned
- `dist/` staleness misled a live check once — rebuild before diffing rendered HTML.
- `gh run watch` can hang on this device; fetch the latest run id, watch with `2>&1 | tail -1`, fall back to `gh run view --json status,conclusion` + `curl` live URL status.
- `pkill -f node/python` kills opencode's own session. Use targeted PID kills only.
- proot Debian chromium crashes (signal 5) — only native `chromium-browser` (Termux) works for screenshots; measurement harness: `dist/harness.html?w=<width>&path=<page>` polling `window.__done`.
- The `theme-slot` spans in the Header must never be removed — the toggle is injected into them at runtime.

## Open threads (ideas, not commitments)
- Optional testimonials/carousel section if user provides quotes (pattern already researched).
- `get-hike.reakinfotech.com` custom domain: DNS still NXDOMAIN — only worth revisiting if user configures DNS.
