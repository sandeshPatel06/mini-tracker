# Project MIND.md - Technical Context & Architecture Memory

This document serves as the primary technical context and architectural memory for `mini-tracker` (branded as **Hike** / **Get-Hike**). Future AI agents and developers should read this document first before inspecting or modifying the codebase.

---

## 1. Project Overview

`mini-tracker` is a lightweight, privacy-aware desktop productivity and telemetry tracker built for Linux (X11 & Wayland support). It periodically captures desktop screenshots and hardware telemetry (keystroke entropy, mouse clicks, mouse movement distance) to provide automatic productivity scoring, activity categorizations, and detailed interactive analytics.

### Main Purpose
To provide developers, remote workers, and teams with accurate, automated, AI-assisted productivity tracking without requiring intrusive keylogging permissions, cloud locks, or high CPU usage.

### Major Features
* **Automated Screenshots & Retention**: Captures active displays at configurable intervals (default: 15 seconds) with a strict 7-day automatic rolling retention cleanup.
* **Hardware Telemetry (Zero-Sudo & Hybrid)**: Tracks keystroke entropy (chaos index), mouse click counts, and mouse pixel distance via Linux `/dev/input/event*` devices or browser fallback handlers.
* **AI Multimodal Vision Analysis**: Uses Google Gemini API (preferred: `gemma-4-31b-it`, `gemma-2-27b-it`) to inspect screenshots in batch, detect active applications, categorize activities (Coding, Browsing, Communication, Entertainment, Idle), and evaluate productivity scores (0–100).
* **Task Goal Context Alignment**: Allows users to set a current task goal (max 20 words). The AI prompt integrates this goal to assess whether desktop activity aligns with the user's intended objective.
* **Multi-Organization & Team Sync**: Supports tenant organization creation, member invites via token, and background API sync (push local logs to central server, pull team activity).
* **Dual Runtime Modes**:
  1. **Desktop App Mode**: Standard Wails v2 desktop application wrapping a React TypeScript frontend.
  2. **Standalone Server Mode**: Headless Go HTTP server (`cmd/server/main.go`) serving REST APIs for multi-tenant web access.

---

## 2. Technology Stack

### Core Languages & Runtimes
* **Go**: `1.21+` (Backend logic, hardware input polling, SQLite ORM, AI client, background cron jobs).
* **TypeScript / React**: `React 18`, `TypeScript 5` (Frontend UI, charts, settings, goal dialogs).
* **CSS / HTML**: Vanilla CSS with customized dark theme CSS variables (`--bg-primary`, `--accent-primary`, glassmorphism UI).

### Frameworks & Libraries
* **Desktop Wrapper**: [Wails v2](https://wails.io) (`github.com/wailsapp/wails/v2`).
* **Database & ORM**: SQLite (`modernc.org/sqlite` or `github.com/mattn/go-sqlite3`) managed with [Ent](https://entgo.io) SQL builder (`entgo.io/ent`).
* **Input Hardware Polling**: `github.com/gordonklaus/inevdev` (Linux event device parser).
* **Frontend Libraries**:
  * `recharts`: Interactive area, bar, pie, and timeline graphs.
  * `lucide-react`: UI icon set.
  * `wailsapp/runtime`: JS bindings for Go-to-Frontend IPC.

### Databases & Storage
* **SQLite 3**: Local database stored at `~/.local/share/get-hike/tracker.db`.
* **File System Storage**: Screenshots saved at `~/.local/share/get-hike/screenshots/`. Logs saved at `~/.local/share/get-hike/logs/`.

### External Services
* **Google Gemini REST API**: Multimodal vision analysis endpoint (`https://generativelanguage.googleapis.com/v1beta/models`).

---

## 3. Project Architecture

The application adopts a decoupled hybrid desktop/web architecture centered around Wails v2 and Ent ORM.

```
+-------------------------------------------------------------------------------+
|                               FRONTEND (React)                                |
|  - Dashboard, Timeline, Analytics, Organization, Settings, Task Goal Dialog   |
|  - Recharts Visualization, Web Mouse Fallback (requestAnimationFrame)         |
+---------------------------------------+---------------------------------------+
                                        | Wails IPC / REST API
+---------------------------------------v---------------------------------------+
|                                BACKEND (Go)                                   |
|                                                                               |
|  +-------------------+  +-------------------+  +---------------------------+  |
|  | Input Tracker     |  | AI Analyzer       |  | Sync & Organization       |  |
|  | - Evdev Polling   |  | - Gemini REST     |  | - Background Cron (3h)    |  |
|  | - Entropy Calc    |  | - Gemma Models    |  | - Server Sync Pull/Push   |  |
|  +---------+---------+  +---------+---------+  +-------------+-------------+  |
|            |                      |                          |                |
|            +----------------------+--------------------------+                |
|                                   |                                           |
|  +--------------------------------v----------------------------------------+  |
|  | Ent ORM Data Layer (ent/client.go)                                      |  |
|  +--------------------------------+----------------------------------------+  |
+-----------------------------------|-------------------------------------------+
                                    | SQLite SQL Driver
+-----------------------------------v-------------------------------------------+
|                          DATABASE & STORAGE (Linux)                           |
|  - SQLite DB: ~/.local/share/get-hike/tracker.db                              |
|  - Image Disk: ~/.local/share/get-hike/screenshots/*.png                       |
+-------------------------------------------------------------------------------+
```

---

## 4. Directory Structure

```
mini-tracker/
├── app.go                      # Core Wails application bridge (frontend methods)
├── main.go                     # Desktop app entrypoint (initializes Wails & DB)
├── install.sh                  # One-line Linux installer & database safe updater
├── go.mod / go.sum             # Go module definition & dependencies
├── ent/                        # Generated Ent ORM entities, schemas, & queries
│   ├── schema/                 # Ent database schemas (Log, User, Org, Invite)
│   └── ...
├── cmd/
│   └── server/
│       └── main.go             # Standalone HTTP REST server entrypoint
├── internal/                   # Private backend Go packages
│   ├── ai/                     # Gemini REST API client & prompt builders
│   ├── config/                 # App configuration & directory management
│   ├── db/                     # DB initialization, safe ALTER TABLE migrations
│   ├── sync/                   # Remote server sync cron & organization logic
│   └── tracker/                # Screenshot capturer & Linux evdev input loggers
│       ├── tracker.go          # Main tracking loop coordinator
│       ├── screenshot.go       # Screenshot capture utility
│       ├── keylog.go           # Hardware keyboard evdev polling & entropy
│       └── mouselog.go         # Hardware mouse evdev polling & distance
└── frontend/                   # React TypeScript frontend
    ├── package.json            # Node.js dependencies & scripts
    ├── wailsjs/                # Auto-generated Wails IPC bindings
    └── src/
        ├── App.tsx             # Root layout, navigation sidebar, task dialog
        ├── pages/              # UI Page Views
        │   ├── Dashboard.tsx   # Live status, task goal setter, score summary
        │   ├── Timeline.tsx    # Chronological screenshot grid & AI details
        │   ├── Analytics.tsx   # Productivity charts & category breakdown
        │   ├── Organization.tsx# Team management & member invite portal
        │   ├── Settings.tsx    # API key, capture intervals, AI model pick
        │   └── AcceptInvite.tsx# Org join flow via invite tokens
        ├── components/         # Shared modal & chart UI components
        └── style.css           # Global CSS rules, colors, and layout tokens
```

---

## 5. Application Flow

```
[ User Input / Mouse / Desktop Screen ]
                │
                ├── (Every 15s) Screenshot Captured & Hardware Evdev Metrics Coalesced
                │
                ▼
      [ Tracker Loop (internal/tracker) ]
                │
                ├── Saves screenshot to ~/.local/share/get-hike/screenshots/
                ├── Calculates Keystroke Entropy & Mouse Pixel Distance
                └── Writes unanalyzed record to SQLite DB
                │
                ▼
      [ AI Batch Processing (internal/ai) ]
                │
                ├── Bundles unanalyzed records (batch size: 3–6)
                ├── Injects Active User Task Goal ("Developing Go Backend")
                ├── Sends JPEG screenshots + telemetry context to Gemini REST API
                └── Receives JSON result (App Name, Category, Productivity 0-100)
                │
                ▼
        [ Ent SQLite DB Persistence ]
                │
                ▼
   [ React Frontend via Wails IPC ] <── (Auto-refreshes Dashboard & Timeline)
```

---

## 6. Backend Deep-Dive

### Key Packages
1. **`main.go` & `app.go`**:
   - Manages application lifecycle. `app.go` exposes Go methods directly to JavaScript (`GetLogs`, `GetAnalytics`, `SetUserTask`, `SaveConfig`, `AnalyzePendingLogs`).
2. **`internal/tracker`**:
   - `keylog.go` & `mouselog.go`: Opens `/dev/input/event*` devices using `evdev`. Polling runs on dedicated background goroutines with `SYN_REPORT` event coalescing to ensure low CPU footprint (<10%).
   - `entropy.go`: Calculates Shannon-style keystroke entropy score normalized (0–100) to measure input diversity versus repetitive typing.
3. **`internal/ai`**:
   - `gemini.go`: Implements `GeminiClient`. Uses HTTP client directly against Gemini v1beta endpoint to avoid heavy SDK dependencies.
   - **Model Hierarchy**: Prefers low-latency efficient vision models like `gemma-4-31b-it` or `gemma-2-27b-it`.
   - **Thinking Model Parser Filter**: Strips out chain-of-thought parts before JSON parsing to avoid syntax errors when using candidate models with reasoning output.
4. **`internal/db`**:
   - `db.go`: Initializes SQLite database at `~/.local/share/get-hike/tracker.db`.
   - **Safe Migration Engine**: SQLite versions below `3.37.0` throw errors on `ALTER TABLE ADD COLUMN IF NOT EXISTS`. `db.go` uses `PRAGMA table_info` checks before safely adding missing columns dynamically.

---

## 7. Frontend Deep-Dive

### Architecture & Components
* **Root Layout (`App.tsx`)**: Controls global sidebar navigation, active page state, and top bar task goal dialog ("What are you working on?").
* **Dashboard (`pages/Dashboard.tsx`)**: Real-time score indicator, telemetry overview, and prompt trigger for analyzing unanalyzed logs.
* **Timeline (`pages/Timeline.tsx`)**: Chronological grid of captured desktop screenshots, telemetry metrics, and AI breakdown badges.
* **Analytics (`pages/Analytics.tsx`)**: Detailed metrics visualization using `recharts` for daily, weekly, and monthly productivity trends.
* **Zero-Sudo Mouse Tracking**: In browser fallback mode, `style.css` / frontend attaches `mousemove` listeners using `requestAnimationFrame` (60fps) to buffer pixel distance, avoiding CPU spikes.

---

## 8. Database Schema & Migrations

The database is built on Ent SQLite. Key entities include:

* **`Log`**:
  * `id`: Integer primary key.
  * `timestamp`: Unix timestamp of capture.
  * `image_path`: Path to stored JPEG screenshot.
  * `total_keys`, `total_clicks`, `mouse_distance`: Raw telemetry values.
  * `entropy_score`: Float keystroke entropy rating (0–100).
  * `app_name`, `window_title`: Detected active application and window title.
  * `productivity_score`: Integer AI rating (0–100).
  * `category`: Work category (Coding, Browsing, Communication, Entertainment, Idle).
  * `analyzed`: Boolean flag indicating whether AI analysis has completed.
* **`User`**: System user metadata, email, and authentication tokens.
* **`Organization`**: Tenant org details for team aggregation.
* **`Invite`**: Member invitation tokens with expiration logic.

---

## 9. Configuration & Environment Variables

Configurations are stored in JSON at `~/.local/share/get-hike/config.json`.

### Config Schema
```json
{
  "api_key": "AIzaSy...",
  "screenshot_interval": 15,
  "retention_days": 7,
  "ai_model": "gemma-4-31b-it",
  "org_id": "",
  "sync_server_url": "https://api.get-hike.com"
}
```

* **No Credentials in Repository**: Secrets are strictly kept inside user local paths (`~/.local/share/get-hike/config.json`). Never commit real API keys or tokens.

---

## 10. Development & Build Workflow

### Prerequisites
* Go 1.21+
* Node.js 18+ & npm
* Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

### Common Commands
* **Run Desktop App in Dev Mode**:
  ```bash
  wails dev
  ```
* **Build Desktop App Binary**:
  ```bash
  wails build
  ```
* **Build Frontend Assets Standalone**:
  ```bash
  cd frontend && npm run build
  ```
* **Run Server Mode**:
  ```bash
  go run cmd/server/main.go
  ```

---

## 11. Technical Decisions & Architectural Rationale

1. **Evdev Zero-Sudo & Hardware Access**: Polling `/dev/input/event*` directly avoids heavy X11/Wayland display server bindings for telemetry. When user permissions restrict evdev, the application falls back to browser frontend input tracking.
2. **Gemini Batching over Single Requests**: Analyzing single screenshots repeatedly burns API rate limits and adds network overhead. `AnalyzeBatch` aggregates 3 to 6 screenshots into one multimodal vision prompt.
3. **Custom SQLite Migration Wrapper**: `Ent` auto-migration can fail on older SQLite shared library versions during schema evolution. The custom `safeAddColumn` helper in `internal/db/db.go` solves schema migration failures safely.
4. **Safe Backup Engine in Installer (`install.sh`)**: When updating application binaries, `install.sh` automatically creates a timestamped database backup (`tracker.db.bak_<timestamp>`) to prevent data loss.

---

## 12. Important Gotchas & Precautions

* **Do NOT modify SQLite schemas directly without using Ent**: Always edit `ent/schema/*.go` and regenerate Ent entities if adding backend fields.
* **Do NOT remove `SYN_REPORT` handling in evdev loggers**: Keyboard and mouse event loops rely on `SYN_REPORT` to flush event packets. Removing this will cause high CPU loops or corrupted distance calculations.
* **Model Response Parsing**: AI models may return thinking candidate blocks. Ensure `parseBatchJSONResponse` in `internal/ai/gemini.go` continues filtering non-text/thought parts before running `json.Unmarshal`.

---

## 13. AI Agent Guidelines for Future Tasks

When working on this repository, future AI agents MUST follow these rules:

1. **Verify Code Before Changing**: Never assume file layout or function signatures without inspecting source files using `view_file` or `grep_search`.
2. **Preserve Database Migration Safety**: If adding new columns to SQLite via `internal/db/db.go`, ensure the `safeAddColumn` helper is used to verify column existence with `PRAGMA table_info` before invoking `ALTER TABLE`.
3. **Keep Telemetry Overhead Low**: Hardware loops in `internal/tracker/keylog.go` and `mouselog.go` must remain non-blocking. Do NOT introduce heavy sync calls inside input loops.
4. **Do NOT Modify Application Source Code for Documentation Tasks**: Keep changes strictly limited to specified targets.
