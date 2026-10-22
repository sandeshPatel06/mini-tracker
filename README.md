# 📍 get-Hike: Open-Source Productivity Tracker with AI Analysis

> A privacy-first, **open-source** productivity tracker for Linux with automated Gemini AI activity analysis, work-session tracking, and an **optional** multi-tenant team organization engine. Self-host it for personal tracking or roll it out across a team.

---

## 🆓 Open Source & Free for Personal Use

get-Hike is free and open-source software released under the [MIT License](#-license). Anyone can use, modify, self-host and redistribute it: **personal use is explicitly welcome**.

- **Free for everyone**: no license fee, subscription or trial: use it for personal tracking at no cost.
- **Solo mode built in**: run it as a private, local tracker in Guest Mode: no account, no organization, no cloud required.
- **Team features are optional**: OAuth 2.0, organizations, member invitations and team analytics are there only if you want them.
- **Your data stays yours**: all data is stored locally in `~/.local/share/get-hike/` on your own machine.
- **Hack on it**: MIT means you can fork it, change it, and share your version with others.

---

## 🌟 Key Features

- ⏱️ **Work Session Tracking & Live Clock**: User-controlled session toggle with real-time timer directly embedded in the dashboard sidebar and standalone floating desktop widget.
- 🤖 **Automated Gemini AI Batch Analysis**: Intelligent batch analysis powered by Google Gemini API (e.g. `gemini-3.5-flash-lite`, `gemini-flash-lite-latest`) with automatic model failover, rate-limit resilience, and configurable periodic cron processing. Each completed 5-minute window is scored as one bundle using its screenshots, key/click counts and app-switch events, so focused reading counts as productive while clearly idle windows skip the API entirely.
- 🔐 **OAuth 2.0 & Multi-Tenant Organization Engine**: Corporate team management supporting Google OAuth 2.0, Azure AD / Microsoft OAuth 2.0, Email/Password login, Role-Based Access Control (Owner, Admin, Member), and time-sensitive secure invitation token links.
- 📊 **Multi-User Analytics & Date Range Filtering**: Inspect productivity timelines, category distributions, and AI ratings across team members with multi-user dropdown filters and date range selection (`start_date`, `end_date`, `user_id`).
- 🪟 **Floating OS Desk Widget (`/wizard`)**: Launch a lightweight, floating desktop widget window for session control and quick activity metrics without needing a full browser tab open.
- 🔒 **Privacy-First Architecture**: Zero keylogging of typed characters: tracks raw key entropy score only. All screenshots and telemetry remain strictly local and are automatically pruned after processing.
- ⚡ **Zero GTK/Desktop Dependencies**: Standalone Go binary serving an embedded React SPA, accessible from any browser without requiring root/sudo privileges.
- ⚙️ **Automated Systemd Integration & One-Line Installer**: Clean `install.sh` / `uninstall.sh` script providing desktop launcher shortcuts (`get-hike`), systemd user service autostart, and smooth updates.

---

## 🏗️ Architecture

```
Mini Tracker / get-Hike Web Application (Port 8080)
├── Go Standalone Server (cmd/server)
│   ├── Keystroke Entropy Engine (/dev/input/event* & API Input Fallback)
│   ├── Screenshot Capturer & Auto-Retention Pruning Engine
│   ├── Stateful Tracker Controller (Start/Pause Session Timer)
│   ├── Background AI Cron Service (Configurable Batch Interval)
│   ├── Gemini REST Client (Auto Model Discovery, Model Failover & Batching)
│   ├── Multi-Tenant SQLite Database (Logs, Users, Orgs, Invitations)
│   └── OAuth 2.0 & Auth Handler (Google, Azure AD / Microsoft, Session Tokens)
└── Embedded React Web Dashboard (frontend/dist)
    ├── Work Session Widget & Floating Desktop Wizard (/wizard)
    ├── Real-Time Dashboard & Multi-User Analytics Filtering
    ├── Productivity Timeline & Screenshot Viewer
    ├── Corporate Organization & Member Invitation Management
    └── System Settings & Gemini API Configuration
```

---

## 🚀 Quickstart & Setup

> **Personal / solo use:** no account and no organization needed: run the app locally in Guest Mode and everything stays on your machine. The team/org features below are completely optional.

### 1. One-Line Automatic Installation (Recommended)

Run the automated installer script to download or build the binary, configure desktop shortcuts, and set up systemd autostart:

```bash
./install.sh
```

To remove or uninstall:
```bash
./uninstall.sh
```

---

### 2. Manual Development & Build Setup

#### Prerequisites
- **Go**: 1.22+
- **Node.js & npm** (for building frontend): Node 18+
- **Input Permissions**: Works out-of-the-box in **Zero-Sudo Mode** (no root or group configuration required).
  - *Optional (Zero-Logout Hardware evdev access)*: To enable kernel-level `/dev/input` tracking without adding your user to the `input` group or logging out:
    ```bash
    make setup-input
    ```

#### Build Instructions
```bash
# 1. Install frontend dependencies and build SPA assets
cd frontend && npm install && npm run build && cd ..

# 2. Build standalone Go server binary
go build -o bin/get-hike-server ./cmd/server

# 3. Launch application
./bin/get-hike-server
```

Once launched, access the dashboard in your browser:
👉 **[http://localhost:8080](http://localhost:8080)**

---

## ⚙️ Configuration

Set environment variables in `.env` or in `~/.config/get-hike/config.json`:

```bash
# Gemini API Key (Get key from https://aistudio.google.com/)
export GEMINI_API_KEY="your-gemini-api-key"

# AI Model Selection (Defaults to gemini-3.5-flash-lite with auto-failover)
export GEMINI_MODEL="gemini-3.5-flash-lite"

# Gemini explicit context caching for repeated analysis instructions (default on)
export GEMINI_CONTEXT_CACHE="true"

# AI Bundle Analysis Interval (each completed 5-minute window is scored as one bundle)
export AI_ANALYSIS_INTERVAL_SECONDS="5m"

# Screenshot Capture Frequency
export SCREENSHOT_INTERVAL_SECONDS="30"

# Server Port & Endpoint
export PORT="8080"
export BACKEND_URL="http://localhost:8080"

# OAuth 2.0 Credentials (Optional)
export GOOGLE_CLIENT_ID="your-google-client-id"
export GOOGLE_CLIENT_SECRET="your-google-client-secret"
export AZURE_CLIENT_ID="your-azure-client-id"
export AZURE_CLIENT_SECRET="your-azure-client-secret"
export AZURE_TENANT_ID="common"

# Web Session Signing Key (server mode; sessions reset on restart when unset)
export JWT_SECRET="a-long-random-secret"
```

Configuration file `~/.config/get-hike/config.json` example:
```json
{
  "gemini_api_key": "your-gemini-api-key",
  "gemini_model": "gemini-3.5-flash-lite",
  "ai_analysis_interval_seconds": 10800,
  "screenshot_interval_seconds": 30,
  "data_dir": "~/.local/share/get-hike",
  "backend_port": 8080,
  "backend_endpoint": "http://localhost:8080"
}
```

---

## 📡 API Reference

### 🔐 Authentication & OAuth
- `POST /api/org/register`: Register new corporate organization & owner account
- `POST /api/auth/login`: Email/password login session creation
- `POST /api/auth/logout`: End active user session
- `GET /api/auth/me`: Fetch currently authenticated user session info
- `GET /api/auth/oauth/google`: Initiate Google OAuth 2.0 authentication
- `GET /api/auth/oauth/azure`: Initiate Azure AD / Microsoft OAuth 2.0 authentication

### ⏱️ Tracker & Desktop Wizard
- `GET /api/tracker/status`: Get active session state and cumulative elapsed timer
- `POST /api/tracker/toggle`: Start / pause work session tracking
- `POST /api/tracker/input`: Submit application-level keystroke metrics
- `GET /wizard`: Standalone floating desktop widget page

### 📊 Logs & Analytics
- `GET /api/logs?date=YYYY-MM-DD&start_date=...&end_date=...&user_id=...`: Retrieve activity logs with date range & user filters
- `GET /api/stats?date=YYYY-MM-DD`: Retrieve aggregated productivity statistics
- `GET /api/screenshots?path=...`: Retrieve cached screenshot asset
- `POST /api/process-pending`: Manually trigger batch AI analysis of unanalyzed logs

### 🏢 Corporate Organization Management
- `GET /api/org/members`: List organization members and roles
- `POST /api/org/invite`: Generate secure, time-sensitive invite token link
- `GET /api/org/invite-info?token=...`: Verify invitation token details
- `POST /api/org/accept-invite`: Complete member account registration via invitation link

### ⚙️ System & Settings
- `GET /api/config`: Get system configuration & AI initialization status
- `GET /health`: Health check endpoint

---

## 🔒 Security & Privacy

- **No Keylogging**: Only keystroke event frequency (entropy score) is evaluated. Individual key codes or text entries are never recorded or stored.
- **Local Storage**: All SQLite data resides locally in `~/.local/share/get-hike/`.
- **Screenshot Cleanup**: Temporary screenshots are pruned automatically after AI batch processing and during 7-day retention routines.

---

## 📜 License

This project is **open source** and released under the [MIT License](LICENSE) © 2026 REAK INFOTECH LLP.

You are free to use get-Hike for **personal use** (and any other purpose): run it, modify it, self-host it and share it. See the [LICENSE](LICENSE) file for the full text.

