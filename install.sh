#!/bin/bash

set -e
set -u
set -o pipefail

# Ensure script always runs from the repository root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR" || { echo "❌ Failed to change to script directory: ${SCRIPT_DIR}"; exit 1; }

# ==============================================================================
# 1. CONFIGURATION & LOGGING
# ==============================================================================
APP_NAME="get-Hike"
APP_NAME_LOWER="get-hike"

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

# Safely kill running mini-tracker/get-hike background processes without killing the installer script ($$)
for pid in $(pgrep -f "mini-tracker|get-hike" 2>/dev/null || true); do
    if [ "$pid" != "$$" ] && [ "$pid" != "$PPID" ]; then
        cmdline=$(cat "/proc/$pid/cmdline" 2>/dev/null | tr '\0' ' ' || echo "")
        if [[ "$cmdline" != *"install.sh"* ]]; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    fi
done

# ==============================================================================
# 4. RETRIEVE OR BUILD APPLICATION BINARY
# ==============================================================================
BINARY_SOURCE=""
CUSTOM_BUILD_PATH="${LOCAL_BUILD_PATH:-${1:-}}"

DEFAULT_REPO_URL="https://github.com/sandeshPatel06/mini-tracker"

LATEST_TAG=""
if command -v gh >/dev/null 2>&1; then
    LATEST_TAG=$(gh release view --json tagName -q .tagName 2>/dev/null || echo "")
fi

if [ -n "${LATEST_TAG}" ]; then
    DEFAULT_DOWNLOAD_URL="${DEFAULT_REPO_URL}/releases/download/${LATEST_TAG}/mini-tracker"
else
    DEFAULT_DOWNLOAD_URL="${DEFAULT_REPO_URL}/releases/latest/download/mini-tracker"
fi

EFFECTIVE_DOWNLOAD_URL="${DOWNLOAD_URL:-${DEFAULT_DOWNLOAD_URL}}"

# If custom path/URL argument was provided in command line
if [ -n "${CUSTOM_BUILD_PATH}" ]; then
    if [[ "${CUSTOM_BUILD_PATH}" =~ ^https?:// ]]; then
        log "📥 Downloading binary from specified URL: ${CUSTOM_BUILD_PATH}..."
        mkdir -p bin
        if curl -L --fail --progress-bar "${CUSTOM_BUILD_PATH}" -o bin/mini-tracker; then
            chmod +x bin/mini-tracker
            BINARY_SOURCE="bin/mini-tracker"
            log "✅ Downloaded binary to: ${BINARY_SOURCE}"
        else
            log "❌ Failed to download binary from URL: ${CUSTOM_BUILD_PATH}"
            exit 1
        fi
    elif [ -f "${CUSTOM_BUILD_PATH}" ]; then
        BINARY_SOURCE="${CUSTOM_BUILD_PATH}"
        log "✅ Using specified custom local build binary: ${BINARY_SOURCE}"
    else
        log "⚠️ Specified path '${CUSTOM_BUILD_PATH}' not found locally. Attempting remote download..."
    fi
fi

# If no local path specified or resolved, download from GitHub release
if [ -z "${BINARY_SOURCE}" ] && [ -n "${EFFECTIVE_DOWNLOAD_URL}" ]; then
    log "📥 No local path provided. Attempting download from GitHub release: ${EFFECTIVE_DOWNLOAD_URL}..."
    mkdir -p bin
    if curl -L --fail --progress-bar "${EFFECTIVE_DOWNLOAD_URL}" -o bin/mini-tracker; then
        chmod +x bin/mini-tracker
        BINARY_SOURCE="bin/mini-tracker"
        log "✅ Downloaded release binary from GitHub to: ${BINARY_SOURCE}"
    else
        log "ℹ GitHub release binary not found at ${EFFECTIVE_DOWNLOAD_URL}. Searching local workspace..."
    fi
fi

# Fallback to local workspace pre-built binaries or build desktop binary from source
if [ -z "${BINARY_SOURCE}" ]; then
    if [ -f "./mini-tracker" ]; then
        BINARY_SOURCE="./mini-tracker"
        log "✅ Found application binary: ${BINARY_SOURCE}"
    elif [ -f "bin/mini-tracker" ]; then
        BINARY_SOURCE="bin/mini-tracker"
        log "✅ Found local pre-built application binary: ${BINARY_SOURCE}"
    elif [ -f "build/bin/mini-tracker" ]; then
        BINARY_SOURCE="build/bin/mini-tracker"
        log "✅ Found build binary: ${BINARY_SOURCE}"
    elif [ -f "build/bin/mini-tracker-tmp" ]; then
        BINARY_SOURCE="build/bin/mini-tracker-tmp"
        log "✅ Found build binary: ${BINARY_SOURCE}"
    else
        log "ℹ No pre-built binary found. Attempting build from source..."
        if ! command -v go >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
            log "❌ ERROR: Pre-built binary not found and developer tools (go/npm) are missing."
            exit 1
        fi
        log "Building React Web Dashboard..."
        (cd frontend && npm install && npm run build) || { log "❌ Failed to build frontend"; exit 1; }
        log "Building Go desktop application binary..."
        mkdir -p bin
        go build -o bin/mini-tracker . || { log "❌ Failed to build Go application binary"; exit 1; }
        BINARY_SOURCE="bin/mini-tracker"
        log "✅ Build complete: ${BINARY_SOURCE}"
    fi
fi

# ==============================================================================
# 5. COPY & INSTALL APPLICATION FILES
# ==============================================================================
log "Installing binary to ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}"
mkdir -p "${BIN_DIR}"

cp -f "${BINARY_SOURCE}" "${INSTALL_DIR}/get-hike"
cp -f "${BINARY_SOURCE}" "${INSTALL_DIR}/mini-tracker"
chmod 755 "${INSTALL_DIR}/get-hike" "${INSTALL_DIR}/mini-tracker"

if [ -d "frontend/dist" ]; then
    log "Copying frontend static assets to ${INSTALL_DIR}/frontend/dist..."
    mkdir -p "${INSTALL_DIR}/frontend/dist"
    cp -r frontend/dist/* "${INSTALL_DIR}/frontend/dist/" 2>/dev/null || true
fi

# Create symlinks for desktop application binary
ln -sf "${INSTALL_DIR}/get-hike" "${BIN_DIR}/get-hike"
ln -sf "${INSTALL_DIR}/mini-tracker" "${BIN_DIR}/mini-tracker"

# ==============================================================================
# 6. GUI LAUNCHER SCRIPT SETUP
# ==============================================================================
GUI_LAUNCHER="${INSTALL_DIR}/get-hike-gui"
cat << EOF > "${GUI_LAUNCHER}"
#!/usr/bin/env bash
exec "${INSTALL_DIR}/get-hike" "\$@"
EOF
chmod 755 "${GUI_LAUNCHER}"

# Symlink GUI launcher
ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/get-hike-gui"
ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/mini-tracker-gui"
log "✅ Installed desktop application launchers."

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
            echo '# get-Hike PATH' >> "${SHELL_RC}"
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

# ==============================================================================
# 9. DESKTOP SHORTCUTS & APP ICON INTEGRATION
# ==============================================================================
log "Creating Desktop launcher shortcuts & app icons..."
mkdir -p "${APPLICATIONS_DIR}"
mkdir -p "${AUTOSTART_DIR}"
mkdir -p "${ICON_DIR}"
mkdir -p "${PIXMAP_DIR}"

# Clean up legacy/alias desktop launchers and duplicate icons
rm -f "${APPLICATIONS_DIR}/mini-tracker.desktop"
rm -f "${APPLICATIONS_DIR}/mini-tracker-server.desktop"
rm -f "${APPLICATIONS_DIR}/mini-tracker-tmp.desktop"
rm -f "${APPLICATIONS_DIR}/localhost.desktop"
rm -f "${APPLICATIONS_DIR}/get-Hike ("*.desktop 2>/dev/null || true
rm -f "${APPLICATIONS_DIR}/get-hike ("*.desktop 2>/dev/null || true

rm -f "${ICON_DIR}/mini-tracker.png" "${ICON_DIR}/mini-tracker-server.png" "${ICON_DIR}/mini-tracker-tmp.png" "${ICON_DIR}/localhost.png"
rm -f "${PIXMAP_DIR}/mini-tracker.png" "${PIXMAP_DIR}/mini-tracker-server.png" "${PIXMAP_DIR}/mini-tracker-tmp.png" "${PIXMAP_DIR}/localhost.png"

# Install application icon across system & user icon paths
ICON_SOURCE=""
if [ -f "frontend/src/assets/logo.png" ]; then
    ICON_SOURCE="frontend/src/assets/logo.png"
elif [ -f "build/appicon.png" ]; then
    ICON_SOURCE="build/appicon.png"
elif [ -f "frontend/src/assets/images/logo-universal.png" ]; then
    ICON_SOURCE="frontend/src/assets/images/logo-universal.png"
fi

INSTALLED_ICON_PATH="${INSTALL_DIR}/get-hike.png"
if [ -n "${ICON_SOURCE}" ]; then
    cp -f "${ICON_SOURCE}" "${INSTALLED_ICON_PATH}"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/get-hike.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/get-hike.png"
    log "✅ Installed custom app icon."
fi

# Create Primary get-Hike .desktop entry
DESKTOP_ENTRY_FILE="${APPLICATIONS_DIR}/get-hike.desktop"
cat << EOF > "${DESKTOP_ENTRY_FILE}"
[Desktop Entry]
Type=Application
Name=get-Hike
GenericName=Productivity Tracker & AI Analyzer
Comment=Privacy-first Linux Productivity Tracker & AI Analyzer
Exec=${BIN_DIR}/get-hike %u
Icon=${INSTALLED_ICON_PATH}
Terminal=false
Categories=Utility;Development;Office;
Keywords=productivity;tracker;time;analytics;get-hike;
StartupNotify=true
StartupWMClass=get-hike
WMClass=get-hike
EOF
chmod 755 "${DESKTOP_ENTRY_FILE}"

AUTOSTART_FILE="${AUTOSTART_DIR}/get-hike.desktop"
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
# 10. CLEANUP & DESKTOP USER SPACE REGISTRATION
# ==============================================================================
log "Configuring desktop client autostart..."

# Remove legacy server systemd user service if present
if command -v systemctl >/dev/null 2>&1; then
    if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
        sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user disable --now "${APP_NAME_LOWER}.service" 2>/dev/null || true
    else
        systemctl --user disable --now "${APP_NAME_LOWER}.service" 2>/dev/null || true
    fi
    rm -f "${SERVICE_FILE}"
fi

# ==============================================================================
# 11. HARDWARE PERMISSIONS & CHOWN FIXES
# ==============================================================================
if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
    chown -R "${REAL_USER}:${REAL_GROUP}" "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" "${AUTOSTART_FILE}" "${ICON_DIR}" "${PIXMAP_DIR}" 2>/dev/null || true
    if [ "${APPLICATIONS_DIR}" = "${REAL_HOME}/.local/share/applications" ]; then
        chown -R "${REAL_USER}:${REAL_GROUP}" "${APPLICATIONS_DIR}" 2>/dev/null || true
    fi
    # Install zero-logout udev rule for instant hardware input access without requiring logout or 'usermod -aG input'
    UDEV_RULE_SRC="$(dirname "$0")/scripts/99-get-hike-input.rules"
    if [ ! -f "${UDEV_RULE_SRC}" ]; then
        UDEV_RULE_SRC="$(dirname "$0")/scripts/99-mini-tracker-input.rules"
    fi
    if [ -f "${UDEV_RULE_SRC}" ] && [ -d "/etc/udev/rules.d" ]; then
        cp "${UDEV_RULE_SRC}" "/etc/udev/rules.d/99-get-hike-input.rules" 2>/dev/null || true
        udevadm control --reload-rules 2>/dev/null || true
        udevadm trigger 2>/dev/null || true
        log "✅ Installed udev rule for zero-logout hardware input access."
    fi

    # Set file capabilities on desktop app binary as additional zero-group option
    if command -v setcap >/dev/null 2>&1 && [ -f "${INSTALL_DIR}/get-hike" ]; then
        setcap cap_dac_read_search+ep "${INSTALL_DIR}/get-hike" 2>/dev/null || true
    fi
else
    log "ℹ️ Running as user without sudo: Zero-Sudo Application & API input tracking enabled automatically."
fi

# Verify Installation
if [ -f "${INSTALL_DIR}/get-hike" ]; then
    log "🎉 ${APP_NAME} desktop application installation completed successfully."
    log "Log saved to: ${LOG_FILE}"
else
    log "❌ Installation failed."
    exit 3
fi

exit 0
