# 📍 Mini Tracker

> A privacy-first Linux productivity tracker — keystroke entropy + AI-powered screenshot analysis.

## Architecture

```
Wails Desktop App (native window)
├── Go Backend
│   ├── Keystroke tracker (evdev → /dev/input/event*)
│   ├── Screenshot daemon (every 30s, 720p JPEG @60%)
│   ├── Gemini 1.5 Flash AI analysis
│   └── SQLite storage
└── React Frontend (Dashboard / Timeline / Analytics)

Docker Backend (optional, for API access)
└── REST API over shared SQLite DB
```

## Prerequisites

### System
```bash
sudo apt-get install -y pkg-config libwebkit2gtk-4.1-dev gcc
```

### Input Group (for keystroke tracking)
```bash
sudo usermod -aG input $USER
# Log out and back in for this to take effect
```

### Wails CLI
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

## Configuration

Set your Gemini API key (get it from https://aistudio.google.com/):

```bash
export GEMINI_API_KEY="your-key-here"
```

Or create `~/.config/mini-tracker/config.json`:
```json
{
  "gemini_api_key": "your-key-here",
  "screenshot_interval_seconds": 30,
  "data_dir": "~/.local/share/mini-tracker"
}
```

## Running

### Desktop App (main way)
```bash
make dev        # hot reload dev mode
make build      # production binary → build/bin/mini-tracker
```

### Docker Backend (optional REST API)
```bash
export GEMINI_API_KEY="your-key"
make docker-up   # starts API on http://localhost:8080
make docker-logs # follow logs
make docker-down # stop
```

API endpoints:
- `GET /api/logs?date=2026-04-28` — all captures for a date
- `GET /api/stats?date=2026-04-28` — productivity summary
- `GET /health` — health check

## Data Location

All data is stored locally:
```
~/.local/share/mini-tracker/
├── tracker.db           ← SQLite database
└── images/
    └── YYYY-MM-DD/
        └── HH-MM-SS.jpg ← compressed screenshots
```

## Privacy

- **No key content is recorded** — only total and unique key counts per interval
- All data stays local (SQLite on your machine)
- Screenshots are compressed to 720p JPEG @60% quality
- AI analysis only uses the screenshot + keystroke count, never individual keys

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Desktop Framework | [Wails v2](https://wails.io) |
| Backend Language | Go 1.25 |
| Frontend | React 18 + TypeScript + Vite |
| Charts | Recharts |
| Keystroke Tracking | [holoplot/go-evdev](https://github.com/holoplot/go-evdev) |
| Screenshot | [kbinani/screenshot](https://github.com/kbinani/screenshot) |
| Database | SQLite (mattn/go-sqlite3) |
| AI | Google Gemini 1.5 Flash |
| Docker | Alpine multi-stage |
