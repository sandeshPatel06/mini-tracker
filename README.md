# 📍 Mini Tracker — Corporate Beta Platform

> A privacy-first, zero-dependency Linux productivity tracking web platform with automated Gemini AI activity analysis, stateful Work Session management, and multi-tenant team organization engine.

---

## 🌟 Key Features

- ⏱️ **Work Session Tracking & Live Clock**: User-controlled session toggle and real-time timer directly embedded in the dashboard sidebar.
- 🤖 **Automated AI Analysis Cron**: Automated background batch analysis job (default every 3 hours, fully configurable) powered by Google Gemini API with smart model failover.
- 🏢 **Multi-Tenant Organization Engine**: Corporate team management featuring Role-based access control (Owner, Admin, Member) and secure time-sensitive invitation token links.
- 🔒 **Privacy-First Architecture**: Zero keylogging of typed content—tracks raw entropy score only. All screenshots and telemetry remain strictly local until processed.
- ⚡ **Zero GTK/Desktop Dependencies**: Runs as a lightweight standalone web binary serving an embedded React SPA, accessible from any browser without root/sudo required.
- 📊 **Productivity Insights & Timeline**: Visually inspect work timelines, category breakdowns, and AI productivity ratings with zero manual trigger requirements.

---

## 🏗️ Architecture

```
Mini Tracker Web Application (Port 8080)
├── Go Standalone Server (cmd/server)
│   ├── Keystroke Entropy Engine (/dev/input/event*)
│   ├── Screenshot Capturer (720p JPEG @ 60% quality)
│   ├── Stateful Tracker Controller (Start/Pause Session)
│   ├── Background AI Cron Service (Configurable 3h Interval)
│   ├── Organization & Multi-Tenant SQLite Database
│   └── Google Gemini REST Client (Auto Model Discovery & JSON Enforcement)
└── Embedded React Web Dashboard (frontend/dist)
    ├── Work Session Widget & Session Timer
    ├── Real-Time Dashboard & Productivity Timeline
    ├── Analytics & Category Visualizations
    └── Corporate Organization & Member Invitation Management
```

---

## 🚀 Quickstart & Setup

### 1. Prerequisites

- **Go**: 1.22+
- **Node.js & npm** (only for building frontend): Node 18+
- **Input Group Permissions** *(optional, for keystroke entropy)*:
  ```bash
  sudo usermod -aG input $USER
  # Log out and log back in for group membership to take effect
  ```

### 2. Configuration

Create or configure your environment variables in `.env` or set `GEMINI_API_KEY`:

```bash
# Set Gemini API Key (Get key from https://aistudio.google.com/)
export GEMINI_API_KEY="your-gemini-api-key"

# Optional configuration overrides
export AI_ANALYSIS_INTERVAL="3h"          # Frequency of background AI analysis batch
export SCREENSHOT_INTERVAL_SECONDS="30"  # Screenshot capture frequency
export PORT="8080"                       # Web server port
```

Alternatively, configure `~/.config/mini-tracker/config.json`:
```json
{
  "gemini_api_key": "your-gemini-api-key",
  "ai_analysis_interval": "3h",
  "screenshot_interval_seconds": 30,
  "data_dir": "~/.local/share/mini-tracker"
}
```

---

## 🛠️ Building & Running

### Build Web Binary & Embedded Frontend

```bash
# 1. Install frontend dependencies and build assets
cd frontend && npm install && npm run build && cd ..

# 2. Build standalone Go server binary
go build -o bin/mini-tracker-server ./cmd/server

# 3. Launch application
./bin/mini-tracker-server
```

Once launched, access the dashboard in your browser:
👉 **[http://localhost:8080](http://localhost:8080)**

---

## 📡 API Reference

### Tracker State
- `GET /api/tracker/status` — Get tracking status and current session duration
- `POST /api/tracker/toggle` — Toggle tracker (Start/Pause work session)

### Organization & Team Beta
- `GET /api/org` — Fetch organization details & active user list
- `POST /api/org/invite` — Generate secure team member invitation link
- `GET /api/org/invite/info?token=...` — Verify invitation token status
- `POST /api/org/accept-invite` — Complete member registration via token

### Activity Data
- `GET /api/logs?date=YYYY-MM-DD` — Retrieve captured logs & AI classifications
- `GET /api/stats?date=YYYY-MM-DD` — Get aggregated productivity statistics
- `GET /health` — System & AI engine health check

---

## 🔒 Security & Privacy

- **No Keylogging**: Only keystroke event frequency (entropy score) is evaluated. Individual key values are never recorded or stored.
- **Local Data Storage**: All logs and SQLite data reside in `~/.local/share/mini-tracker/`.
- **Automatic Screenshot Cleanup**: Screenshots are automatically pruned after background AI batch analysis completes.

---

## 📜 License

MIT License © REAK INFOTECH LLP
