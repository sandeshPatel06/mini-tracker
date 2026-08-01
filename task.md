This is a fantastic project. Building a productivity tracker in Go for Linux is a great way to leverage Go's concurrency for background tasks while interacting with low-level system APIs. 

To give your AI coding agent (and yourself) the highest chance of success, we will use **Wails** for the desktop application framework. Wails allows you to write the heavy-lifting backend logic in Go while building a beautiful, interactive analytics frontend using web technologies like **React** or **Vue**.

Here is the full, step-by-step master plan and architecture document you can feed directly to an AI developer agent to execute this build.

***

# 🏗️ Architecture & AI Agent Execution Plan: Go Linux Productivity Tracker

**System Architecture Overview:**
* **Backend/Daemon:** Go (Golang)
* **Desktop UI Framework:** Wails (Go + React/Vue)
* **Database:** SQLite (local, embedded storage for logs)
* **Input Tracking:** `golang-evdev` (Linux-native input event reading)
* **Screen Capture:** `github.com/kbinani/screenshot`
* **AI Integration:** REST HTTP calls to a Multimodal LLM (e.g., Gemini, OpenAI, Claude)

---

### Phase 1: Project Initialization & Wails Setup
**Agent Instructions:**
1.  Initialize a new Wails project using a React or Vue template: `wails init -n productivity-tracker -t react-ts`.
2.  Set up the Go backend structure. Create separate packages: `tracker` (for background daemon), `db` (for SQLite operations), and `ai` (for API integration).
3.  Ensure Linux dependencies for Wails are documented in the README (e.g., `libgtk-3-dev`, `libwebkit2gtk-4.0-dev` for Ubuntu/Debian based systems).

### Phase 2: Linux-Native Keystroke "Uniqueness" Tracker
**Agent Instructions:**
1.  Implement a keystroke monitor using `github.com/gvalkov/golang-evdev` to read directly from `/dev/input/event*` devices. This bypasses X11/Wayland display server issues and works at the kernel level.
2.  **Privacy-First Uniqueness Algorithm:** Instead of a malicious keylogger that records exact words, create a function that runs on a 1-minute ticker. 
3.  During that minute, collect all key press events. Calculate the **Unique Key Entropy**:
    * Count the total number of keystrokes.
    * Count the number of *distinct* keys pressed (e.g., typing "aaaa" = 4 total, 1 unique. Typing "func" = 4 total, 4 unique).
    * Calculate a "Productivity Score" based on high total volume + high unique volume (indicating actual typing/coding vs. holding down a single key to fake activity).
4.  Flush this data structure every 60 seconds.

### Phase 3: The Screenshot Daemon
**Agent Instructions:**
1.  Integrate `github.com/kbinani/screenshot`.
2.  Create a Go routine with a `time.Ticker` set to 1 minute.
3.  On every tick, capture the primary display.
4.  Compress the image to a low-quality JPEG (e.g., 60% quality) and scale it down to 720p to save local disk space and reduce the payload size for the AI API.
5.  Convert the compressed image buffer to a Base64 string for immediate AI processing, and save a copy locally in a `~/.local/share/productivity-tracker/images/` directory.

### Phase 4: Data Pipeline & AI Analysis
**Agent Instructions:**
1.  Create an HTTP client in the `ai` package to communicate with the chosen Multimodal LLM API.
2.  **The Prompt Engineering:** Formulate a strict prompt: 
    * *Payload:* Base64 Image + Keystroke Uniqueness Score.
    * *Prompt:* "Analyze the attached screenshot of a Linux desktop. The user had a keystroke uniqueness score of X/100 in the last minute. Categorize the current activity (e.g., Coding, Browsing, Social Media, Idle). Assess if this looks productive. Return ONLY a strict JSON object: `{"category": "string", "productive": boolean, "confidence": number, "brief_reason": "string"}`."
3.  Execute this API call asynchronously every minute right after the screenshot and key capture.

### Phase 5: Local Storage (SQLite)
**Agent Instructions:**
1.  Integrate `github.com/mattn/go-sqlite3`.
2.  Initialize a local SQLite database on app startup.
3.  Create a `logs` table: `id (PK)`, `timestamp`, `image_path`, `total_keys`, `unique_keys`, `ai_category`, `is_productive`, `ai_reason`.
4.  Write a repository pattern in Go to insert the minute-by-minute results into this database.
5.  Write getter functions (e.g., `GetLogsForToday()`, `GetProductivityStats()`) and bind these Go methods to the Wails frontend so the UI can call them directly.

### Phase 6: Frontend Analytics Dashboard (React/Vue)
**Agent Instructions:**
1.  Build a dashboard UI that queries the Wails backend for the SQLite data.
2.  Create a **Timeline View**: A scrolling list of the day's minutes, showing the thumbnail of the screenshot, the AI's category, and the keystroke uniqueness score.
3.  Create an **Analytics View**: Use a charting library (like Recharts or Chart.js) to show a bar chart of "Productive vs. Unproductive" time and a line graph of "Keystroke Entropy" throughout the day.

### Phase 7: Linux Permissions & Deployment
**Agent Instructions:**
1.  Document the permissions required. Because `evdev` requires reading `/dev/input`, the user must be added to the `input` group: `sudo usermod -aG input $USER` (a reboot is required after this).
2.  Configure the Wails build script to generate a `.desktop` file and a standard Linux AppImage or binary.