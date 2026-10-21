# AGENTS.md — working rules for AI agents on this website

Project: **get-Hike** marketing site (`/website` folder of `sandeshPatel06/mini-tracker`).
Published at: `https://sandeshpatel06.github.io/mini-tracker/` (auto-deploy from `main`).

## Stack
- Astro (static output) + Tailwind CSS v4 (via Vite plugin) + TypeScript.
- Fonts: `@fontsource-variable/inter` + `@fontsource-variable/jetbrains-mono` (self-hosted — never add CDN font/asset links).
- No client framework, no `alert()/prompt()/confirm()` anywhere.

## Commands (run from `website/`)
```bash
npm ci                 # or npm install
node scripts/responsive-audit.js   # MUST end with "0 error(s), 0 warning(s)" before commit
npm run build          # 7 pages; verify no warnings in output
```

## Hard rules
1. **Base path:** `astro.config.mjs` has `site: 'https://sandeshpatel06.github.io'` and `base: '/mini-tracker'`. Every internal link must start with `/mini-tracker/`. NEVER remove/change the base.
2. **Theme:** dark is default; light palette is remapped under `[data-theme="light"]` in `src/styles/global.css`. Use Tailwind palette utility classes only — **no raw hex colors** on elements.
3. **On-brand text:** text sitting on emerald-filled surfaces must use `text-on-brand` (custom token), not `text-slate-950`.
4. **Accessibility:** interactive targets ≥ 44px (`size-11` for icon buttons; expand hit-area with `before:` pseudo if the visual control is small), ≥ 8px gap between targets, decorative SVGs get `aria-hidden="true"`, stateful controls expose `aria-expanded` / `aria-pressed` / `aria-controls`. Focus-visible rings are never removed.
5. **Icons:** consistent stroke SVG family (lucide-style, `stroke-width="2"`). Emojis are never used as structural icons.
6. **Contrast:** body text is `text-slate-400` or lighter on dark surfaces; both themes must pass 4.5:1. Never downgrade text colors to fix a layout issue.
7. **Images:** always set `width` + `height` attributes; below-fold images get `loading="lazy"`.
8. **Perf:** animations must respect the existing `prefers-reduced-motion` override in `global.css`.
9. **Changelog:** `src/pages/changelog.astro` is a **static hand-written list** (a live GitHub-Releases fetch was tried and reverted by user decision). Do not reintroduce dynamic fetching.
10. **Privacy:** no analytics, no third-party scripts, no external requests at runtime (api.github.com included). Privacy promises in `privacy.astro` must stay true.

## Git / CI
- `main` is source of truth; small focused commits; **never force-push `main`**.
- `.github/workflows/deploy-website.yml` deploys on push to `main` touching `website/**` — verify with:
  `gh run watch <id> --repo sandeshPatel06/mini-tracker --exit-status` then `curl -s -o /dev/null -w '%{http_code}' https://sandeshpatel06.github.io/mini-tracker/<page>/`.
- `.github/workflows/build.yml` (desktop release binaries) is `workflow_dispatch` only — do not re-add push triggers.
- `dist/` is gitignored; the local build may be stale — **always rebuild before visual verification**.

## Environment notes (Termux host)
- Live-serve a build: `node /data/data/com.termux/files/usr/tmp/opencode/static-server.js website/dist 8080`.
- Chromium smoke tests are possible but flaky on this phone; prefer the audit script + reading rendered HTML. Never run `pkill -f ...` patterns (has killed the tool session).
