#!/bin/bash

set -e
set -u

# ==============================================================================
# 1. CONFIGURATION & LOGGING
# ==============================================================================
APP_NAME="Mini Tracker"
APP_NAME_LOWER="mini-tracker"

# Detect execution context (sudo vs non-sudo)
if [ "${EUID:-$(id -u)}" -eq 0 ]; then
    IS_SUDO=true
    REAL_USER="${SUDO_USER:-root}"
    if [ "${REAL_USER}" != "root" ]; then
        REAL_HOME=$(getent passwd "${REAL_USER}" | cut -d: -f6)
        REAL_UID=$(id -u "${REAL_USER}")
        REAL_GROUP=$(id -gn "${REAL_USER}")
    else
        REAL_HOME="${HOME}"
        REAL_UID=0
        REAL_GROUP="root"
    fi
    INSTALL_DIR="/opt/${APP_NAME_LOWER}"
    BIN_DIR="/usr/local/bin"
    APPLICATIONS_DIR="/usr/share/applications"
    ICON_DIR="/usr/share/icons/hicolor/512x512/apps"
    PIXMAP_DIR="/usr/share/pixmaps"
    LOG_FILE="/var/log/${APP_NAME_LOWER}_install.log"
else
    IS_SUDO=false
    REAL_USER="${USER}"
    REAL_HOME="${HOME}"
    REAL_UID=$(id -u)
    REAL_GROUP=$(id -gn "${USER}" 2>/dev/null || echo "${USER}")
    INSTALL_DIR="${REAL_HOME}/.local/share/${APP_NAME_LOWER}/app"
    BIN_DIR="${REAL_HOME}/.local/bin"
    APPLICATIONS_DIR="${REAL_HOME}/.local/share/applications"
    ICON_DIR="${REAL_HOME}/.local/share/icons/hicolor/512x512/apps"
    PIXMAP_DIR="${REAL_HOME}/.local/share/pixmaps"
    LOG_FILE="${REAL_HOME}/.local/share/${APP_NAME_LOWER}/install.log"
fi

CONFIG_DIR="${REAL_HOME}/.config/${APP_NAME_LOWER}"
DATA_DIR="${REAL_HOME}/.local/share/${APP_NAME_LOWER}"
AUTOSTART_DIR="${REAL_HOME}/.config/autostart"
SYSTEMD_USER_DIR="${REAL_HOME}/.config/systemd/user"
SERVICE_FILE="${SYSTEMD_USER_DIR}/${APP_NAME_LOWER}.service"

mkdir -p "$(dirname "$LOG_FILE")"

log() {
    echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')] $1" | tee -a "$LOG_FILE"
}

log "🚀 Starting installation of ${APP_NAME}..."
log "Target User: ${REAL_USER} (Home: ${REAL_HOME}, Install Dir: ${INSTALL_DIR})"

# ==============================================================================
# 2. PREREQUISITE CHECKS
# ==============================================================================
if ! command -v curl >/dev/null 2>&1; then
    log "Installing missing prerequisite: curl..."
    if [ "${IS_SUDO}" = true ]; then
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq || true
            apt-get install -y curl
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y curl
        elif command -v yum >/dev/null 2>&1; then
            yum install -y curl
        elif command -v pacman >/dev/null 2>&1; then
            pacman -Sy --noconfirm curl
        fi
    else
        log "⚠️ 'curl' is missing. Please ask your administrator to install curl."
    fi
fi

# ==============================================================================
# 3. CLEANUP OLD INSTANCES
# ==============================================================================
log "Stopping any running ${APP_NAME} processes..."
if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
    sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user stop "${APP_NAME_LOWER}.service" 2>/dev/null || true
else
    systemctl --user stop "${APP_NAME_LOWER}.service" 2>/dev/null || true
fi

# Kill exact running binary process without matching installer arguments
pkill -9 -x "${APP_NAME_LOWER}-server" 2>/dev/null || true

# ==============================================================================
# 4. RETRIEVE OR BUILD APPLICATION BINARY
# ==============================================================================
BINARY_SOURCE=""
CUSTOM_BUILD_PATH="${LOCAL_BUILD_PATH:-${1:-}}"

DEFAULT_REPO_URL="https://github.com/sandeshPatel06/mini-tracker"
DEFAULT_DOWNLOAD_URL="${DEFAULT_REPO_URL}/releases/download/latest/mini-tracker-server"
EFFECTIVE_DOWNLOAD_URL="${DOWNLOAD_URL:-${DEFAULT_DOWNLOAD_URL}}"

if [ -n "${CUSTOM_BUILD_PATH}" ] && [ -f "${CUSTOM_BUILD_PATH}" ]; then
    BINARY_SOURCE="${CUSTOM_BUILD_PATH}"
    log "✅ Using specified custom local build binary: ${BINARY_SOURCE}"
elif [ -n "${EFFECTIVE_DOWNLOAD_URL}" ]; then
    log "📥 Downloading binary from release URL: ${EFFECTIVE_DOWNLOAD_URL}..."
    mkdir -p bin
    if curl -sSL --fail "${EFFECTIVE_DOWNLOAD_URL}" -o bin/mini-tracker-server 2>/dev/null; then
        chmod +x bin/mini-tracker-server
        BINARY_SOURCE="bin/mini-tracker-server"
        log "✅ Downloaded release binary to: ${BINARY_SOURCE}"
    else
        log "⚠️ Could not download release binary from ${EFFECTIVE_DOWNLOAD_URL}. Falling back to local search/build..."
    fi
fi

if [ -z "${BINARY_SOURCE}" ]; then
    if [ -f "build/bin/mini-tracker" ]; then
    BINARY_SOURCE="build/bin/mini-tracker"
    log "✅ Found build binary: ${BINARY_SOURCE}"
elif [ -f "build/bin/mini-tracker-tmp" ]; then
    BINARY_SOURCE="build/bin/mini-tracker-tmp"
    log "✅ Found build binary: ${BINARY_SOURCE}"
elif [ -f "bin/mini-tracker-server" ]; then
    BINARY_SOURCE="bin/mini-tracker-server"
    log "✅ Found local pre-built application binary: ${BINARY_SOURCE}"
elif [ -f "./mini-tracker-server" ]; then
    BINARY_SOURCE="./mini-tracker-server"
    log "✅ Found local pre-built application binary: ${BINARY_SOURCE}"
elif [ -f "./mini-tracker" ]; then
    BINARY_SOURCE="./mini-tracker"
    log "✅ Found local pre-built application binary: ${BINARY_SOURCE}"
else
    log "ℹ No pre-built binary found. Attempting build from source..."
    if ! command -v go >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
        log "❌ ERROR: Pre-built binary not found and developer tools (go/npm) are missing."
        exit 1
    fi
    log "Building React Web Dashboard..."
    (cd frontend && npm install --silent && npm run build) || { log "❌ Failed to build frontend"; exit 1; }
    log "Building Go binary..."
    mkdir -p bin
    go build -o bin/mini-tracker-server ./cmd/server || { log "❌ Failed to build Go binary"; exit 1; }
    BINARY_SOURCE="bin/mini-tracker-server"
    log "✅ Build complete: ${BINARY_SOURCE}"
fi
fi

# ==============================================================================
# 5. COPY & INSTALL APPLICATION FILES
# ==============================================================================
log "Installing binary to ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}"
mkdir -p "${BIN_DIR}"

cp -f "${BINARY_SOURCE}" "${INSTALL_DIR}/mini-tracker-server"
chmod 755 "${INSTALL_DIR}/mini-tracker-server"

if [ -d "frontend/dist" ]; then
    log "Copying frontend static assets to ${INSTALL_DIR}/frontend/dist..."
    mkdir -p "${INSTALL_DIR}/frontend/dist"
    cp -r frontend/dist/* "${INSTALL_DIR}/frontend/dist/"
fi

# Create symlink for backend server binary
ln -sf "${INSTALL_DIR}/mini-tracker-server" "${BIN_DIR}/mini-tracker-server"

# ==============================================================================
# 6. GUI LAUNCHER SCRIPT SETUP (Standalone Desktop App Window)
# ==============================================================================
GUI_LAUNCHER="${INSTALL_DIR}/mini-tracker-gui"
cat << 'EOF' > "${GUI_LAUNCHER}"
#!/usr/bin/env bash
ENV_PATH="$HOME/.config/mini-tracker/.env"
if [ -f "$ENV_PATH" ]; then
    set -a; source "$ENV_PATH"; set +a
fi

ENDPOINT="http://localhost:8080"
PROFILE_DIR="$HOME/.config/mini-tracker/browser-profile"
mkdir -p "$PROFILE_DIR"

# Check if server is reachable or process is running before starting background server
if ! curl -s --head "${ENDPOINT}" >/dev/null 2>&1 && ! pgrep -x "mini-tracker-server" >/dev/null 2>&1; then
    if command -v mini-tracker-server >/dev/null 2>&1; then
        mini-tracker-server &
    elif [ -f "$HOME/.local/bin/mini-tracker-server" ]; then
        "$HOME/.local/bin/mini-tracker-server" &
    elif [ -f "/usr/local/bin/mini-tracker-server" ]; then
        "/usr/local/bin/mini-tracker-server" &
    elif [ -f "/opt/mini-tracker/mini-tracker-server" ]; then
        "/opt/mini-tracker/mini-tracker-server" &
    fi
    sleep 1
fi

FLAGS="--user-data-dir=${PROFILE_DIR} --app=${ENDPOINT} --class=mini-tracker --name=mini-tracker --no-first-run --no-default-browser-check"

if command -v google-chrome >/dev/null 2>&1; then
    exec google-chrome ${FLAGS} "$@"
elif command -v google-chrome-stable >/dev/null 2>&1; then
    exec google-chrome-stable ${FLAGS} "$@"
elif command -v chromium >/dev/null 2>&1; then
    exec chromium ${FLAGS} "$@"
elif command -v chromium-browser >/dev/null 2>&1; then
    exec chromium-browser ${FLAGS} "$@"
elif command -v brave-browser >/dev/null 2>&1; then
    exec brave-browser ${FLAGS} "$@"
elif command -v microsoft-edge >/dev/null 2>&1; then
    exec microsoft-edge ${FLAGS} "$@"
else
    exec xdg-open "${ENDPOINT}"
fi
EOF
chmod 755 "${GUI_LAUNCHER}"

# Symlink GUI launcher to both 'mini-tracker' and 'mini-tracker-gui'
ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/mini-tracker"
ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/mini-tracker-gui"
log "✅ Installed launcher scripts."

# ==============================================================================
# 7. PATH CONFIGURATION FOR USER SPACE
# ==============================================================================
if [ "${IS_SUDO}" = false ]; then
    SHELL_RC=""
    if [ -f "${REAL_HOME}/.zshrc" ]; then
        SHELL_RC="${REAL_HOME}/.zshrc"
    elif [ -f "${REAL_HOME}/.bashrc" ]; then
        SHELL_RC="${REAL_HOME}/.bashrc"
    fi

    if [ -n "${SHELL_RC}" ]; then
        if ! grep -q 'PATH.*\.local/bin' "${SHELL_RC}"; then
            echo '' >> "${SHELL_RC}"
            echo '# Mini Tracker PATH' >> "${SHELL_RC}"
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "${SHELL_RC}"
            log "✅ Added ${BIN_DIR} to PATH in ${SHELL_RC}"
        fi
    fi
fi

# ==============================================================================
# 8. CONFIGURATION & DATA DIRECTORIES
# ==============================================================================
log "Provisioning directories..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${DATA_DIR}"

ENV_FILE="${CONFIG_DIR}/.env"
if [ ! -f "${ENV_FILE}" ]; then
    cat << EOF > "${ENV_FILE}"
# Mini Tracker Environment Configuration
GEMINI_API_KEY=${GEMINI_API_KEY:-}
SCREENSHOT_INTERVAL_SECONDS=30
AI_ANALYSIS_INTERVAL=3h
EOF
    log "✅ Created environment config file: ${ENV_FILE}"
fi

# ==============================================================================
# 9. DESKTOP SHORTCUTS & APP ICON INTEGRATION
# ==============================================================================
log "Creating Desktop launcher shortcuts & app icons..."
mkdir -p "${APPLICATIONS_DIR}"
mkdir -p "${AUTOSTART_DIR}"
mkdir -p "${ICON_DIR}"
mkdir -p "${PIXMAP_DIR}"

# Install application icon across all system & user icon paths
ICON_SOURCE=""
if [ -f "frontend/src/assets/logo.png" ]; then
    ICON_SOURCE="frontend/src/assets/logo.png"
elif [ -f "build/appicon.png" ]; then
    ICON_SOURCE="build/appicon.png"
elif [ -f "frontend/src/assets/images/logo-universal.png" ]; then
    ICON_SOURCE="frontend/src/assets/images/logo-universal.png"
fi

INSTALLED_ICON_PATH="${INSTALL_DIR}/mini-tracker.png"
if [ -n "${ICON_SOURCE}" ]; then
    cp -f "${ICON_SOURCE}" "${INSTALLED_ICON_PATH}"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/mini-tracker.png"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/mini-tracker-server.png"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/mini-tracker-tmp.png"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/localhost.png"

    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/mini-tracker.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/mini-tracker-server.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/mini-tracker-tmp.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/localhost.png"
    log "✅ Installed custom app icon across system icon directories."
fi

# Create Primary .desktop entry
DESKTOP_ENTRY_FILE="${APPLICATIONS_DIR}/get-hike.desktop"
cat << EOF > "${DESKTOP_ENTRY_FILE}"
[Desktop Entry]
Type=Application
Name=get-Hike
GenericName=Productivity Tracker & AI Analyzer
Comment=Privacy-first Linux Productivity Tracker & AI Analyzer
Exec=${BIN_DIR}/mini-tracker-gui %u
Icon=${INSTALLED_ICON_PATH}
Terminal=false
Categories=Utility;Development;Office;
Keywords=productivity;tracker;time;analytics;get-hike;
StartupNotify=true
StartupWMClass=get-Hike
WMClass=get-Hike
EOF
chmod 755 "${DESKTOP_ENTRY_FILE}"

# Create secondary .desktop aliases to match all potential WM_CLASS window identifiers
for alias_name in mini-tracker mini-tracker-server mini-tracker-tmp localhost get-hike; do
    cat << EOF > "${APPLICATIONS_DIR}/${alias_name}.desktop"
[Desktop Entry]
Type=Application
Name=get-Hike
Exec=${BIN_DIR}/mini-tracker-gui %u
Icon=${INSTALLED_ICON_PATH}
Terminal=false
NoDisplay=true
StartupWMClass=${alias_name}
EOF
    chmod 755 "${APPLICATIONS_DIR}/${alias_name}.desktop"
done

AUTOSTART_FILE="${AUTOSTART_DIR}/mini-tracker.desktop"
cp -f "${DESKTOP_ENTRY_FILE}" "${AUTOSTART_FILE}"
chmod 755 "${AUTOSTART_FILE}"

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${APPLICATIONS_DIR}" 2>/dev/null || true
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$(dirname "${ICON_DIR}")" 2>/dev/null || true
    gtk-update-icon-cache -f -t "${REAL_HOME}/.local/share/icons/hicolor" 2>/dev/null || true
fi
log "✅ Created Desktop & Autostart launchers with custom icon."

# ==============================================================================
# 10. SYSTEMD USER DAEMON REGISTRATION
# ==============================================================================
log "Setting up systemd background user daemon..."
mkdir -p "${SYSTEMD_USER_DIR}"

cat << EOF > "${SERVICE_FILE}"
[Unit]
Description=Mini Tracker Productivity Daemon
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/mini-tracker-server
Restart=always
RestartSec=5s
Environment="DATA_DIR=${DATA_DIR}"
EnvironmentFile=-${ENV_FILE}

[Install]
WantedBy=default.target
EOF

if command -v systemctl >/dev/null 2>&1; then
    if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
        sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user daemon-reload 2>/dev/null || true
        sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user enable --now "${APP_NAME_LOWER}.service" 2>/dev/null || true
    else
        systemctl --user daemon-reload 2>/dev/null || true
        systemctl --user enable --now "${APP_NAME_LOWER}.service" 2>/dev/null || true
    fi
    log "✅ Systemd background daemon configured and activated."
fi

# ==============================================================================
# 11. HARDWARE PERMISSIONS & CHOWN FIXES
# ==============================================================================
if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
    chown -R "${REAL_USER}:${REAL_GROUP}" "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" "${SYSTEMD_USER_DIR}" "${AUTOSTART_FILE}" "${ICON_DIR}" "${PIXMAP_DIR}" 2>/dev/null || true
    if [ "${APPLICATIONS_DIR}" = "${REAL_HOME}/.local/share/applications" ]; then
        chown -R "${REAL_USER}:${REAL_GROUP}" "${APPLICATIONS_DIR}" 2>/dev/null || true
    fi
    if ! groups "${REAL_USER}" | grep -q '\binput\b'; then
        usermod -aG input "${REAL_USER}"
        log "✅ Automatically added '${REAL_USER}' to 'input' hardware group."
    fi
fi

# Verify Installation
if [ -f "${INSTALL_DIR}/mini-tracker-server" ]; then
    log "🎉 ${APP_NAME} installation completed successfully."
    log "Log saved to: ${LOG_FILE}"
else
    log "❌ Installation failed."
    exit 3
fi

exit 0
