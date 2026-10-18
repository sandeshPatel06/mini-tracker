#!/usr/bin/env bash
#
# get-Hike installer — installs the desktop app (and CLI binary) with desktop
# integration, autostart and zero-sudo input tracking support.
#
# Usage:
#   ./install.sh                  # download latest release (or build from source)
#   ./install.sh <path>           # install a local pre-built binary
#   ./install.sh <url>            # download binary from a custom URL
#   ./install.sh --help
#
# Environment:
#   LOCAL_BUILD_PATH   local binary path (overrides positional argument)
#   DOWNLOAD_URL       custom download URL (overrides GitHub release URL)
#
set -euo pipefail

# ==============================================================================
# 0. SCRIPT LOCATION & CLI
# ==============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo ".")"
cd "$SCRIPT_DIR" 2>/dev/null || true

usage() {
    cat <<'EOF'
get-Hike installer — installs the desktop app with desktop integration,
autostart and zero-sudo input tracking support.

Usage:
  ./install.sh                  # download latest release (or build from source)
  ./install.sh <path>           # install a local pre-built binary
  ./install.sh <url>            # download binary from a custom URL
  ./install.sh --help

Environment:
  LOCAL_BUILD_PATH   local binary path (overrides positional argument)
  DOWNLOAD_URL       custom download URL (overrides GitHub release URL)
EOF
}

POSITIONAL_ARG=""
for arg in "$@"; do
    case "$arg" in
        -h|--help)
            usage
            exit 0
            ;;
        --)
            ;;
        -*)
            echo "Unknown option: $arg (use --help)" >&2
            exit 2
            ;;
        *)
            POSITIONAL_ARG="$arg"
            ;;
    esac
done

# ==============================================================================
# 1. CONFIGURATION & LOGGING
# ==============================================================================
APP_NAME="get-Hike"
APP_NAME_LOWER="get-hike"

# Detect execution context (sudo vs non-sudo)
if [ "$(id -u)" -eq 0 ]; then
    IS_SUDO=true
    REAL_USER="${SUDO_USER:-root}"
    if [ "${REAL_USER}" != "root" ]; then
        REAL_HOME="$(getent passwd "${REAL_USER}" 2>/dev/null | cut -d: -f6 || true)"
        if [ -z "${REAL_HOME}" ]; then
            REAL_HOME="$(eval echo "~${REAL_USER}" 2>/dev/null || echo "/home/${REAL_USER}")"
        fi
        REAL_UID="$(id -u "${REAL_USER}" 2>/dev/null || echo 0)"
        REAL_GROUP="$(id -gn "${REAL_USER}" 2>/dev/null || echo "${REAL_USER}")"
    else
        REAL_HOME="${HOME:-/root}"
        REAL_UID=0
        REAL_GROUP="root"
    fi
    INSTALL_DIR="/opt/${APP_NAME_LOWER}"
    BIN_DIR="/usr/local/bin"
    APPLICATIONS_DIR="/usr/share/applications"
    ICON_DIR="/usr/share/icons/hicolor/512x512/apps"
    ICON_THEME_DIR="/usr/share/icons/hicolor"
    PIXMAP_DIR="/usr/share/pixmaps"
    LOG_FILE="/var/log/${APP_NAME_LOWER}_install.log"
else
    IS_SUDO=false
    REAL_USER="$(id -un)"
    REAL_HOME="${HOME:-$(eval echo "~${REAL_USER}" 2>/dev/null || echo "~")}"
    REAL_UID="$(id -u)"
    REAL_GROUP="$(id -gn 2>/dev/null || echo "${REAL_USER}")"
    INSTALL_DIR="${REAL_HOME}/.local/share/${APP_NAME_LOWER}/app"
    BIN_DIR="${REAL_HOME}/.local/bin"
    APPLICATIONS_DIR="${REAL_HOME}/.local/share/applications"
    ICON_DIR="${REAL_HOME}/.local/share/icons/hicolor/512x512/apps"
    ICON_THEME_DIR="${REAL_HOME}/.local/share/icons/hicolor"
    PIXMAP_DIR="${REAL_HOME}/.local/share/pixmaps"
    LOG_FILE="${REAL_HOME}/.local/share/${APP_NAME_LOWER}/install.log"
fi

# Fall back to a writable log location if the primary one is not usable.
if ! mkdir -p "$(dirname "${LOG_FILE}")" 2>/dev/null || ! touch "${LOG_FILE}" 2>/dev/null; then
    LOG_FILE="${REAL_HOME}/.local/share/${APP_NAME_LOWER}/install.log"
    mkdir -p "$(dirname "${LOG_FILE}")" 2>/dev/null || true
fi

log() {
    echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')] $1" | tee -a "${LOG_FILE}"
}

# Run a command as the real (non-root) user, with their session bus when sudo.
sudo_run() {
    if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
        sudo -u "${REAL_USER}" \
            XDG_RUNTIME_DIR="/run/user/${REAL_UID}" \
            DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" \
            "$@"
    else
        "$@"
    fi
}

human_size() {
    if command -v du >/dev/null 2>&1 && [ -e "$1" ]; then
        du -sh "$1" 2>/dev/null | cut -f1 || true
    fi
}

log "🚀 Starting installation of ${APP_NAME}..."
log "Target User: ${REAL_USER} (Home: ${REAL_HOME}, Install Dir: ${INSTALL_DIR})"

CONFIG_DIR="${REAL_HOME}/.config/${APP_NAME_LOWER}"
DATA_DIR="${REAL_HOME}/.local/share/${APP_NAME_LOWER}"
AUTOSTART_DIR="${REAL_HOME}/.config/autostart"
SYSTEMD_USER_DIR="${REAL_HOME}/.config/systemd/user"
SERVICE_FILE="${SYSTEMD_USER_DIR}/${APP_NAME_LOWER}.service"
DB_PATH="${DATA_DIR}/tracker.db"
PID_FILE="${DATA_DIR}/tracker.pid"

# ==============================================================================
# 2. DOWNLOADER DETECTION
# ==============================================================================
DOWNLOADER=""
if command -v curl >/dev/null 2>&1; then
    DOWNLOADER="curl"
elif command -v wget >/dev/null 2>&1; then
    DOWNLOADER="wget"
else
    log "⚠️ Neither 'curl' nor 'wget' is available — remote download and source build fallbacks will be limited."
fi

# download_file <url> <dest> [quiet]
download_file() {
    local url="$1" dest="$2" quiet="${3:-}" tmp="${2}.part"
    if [ -z "${DOWNLOADER}" ]; then
        return 1
    fi
    mkdir -p "$(dirname "${dest}")"
    rm -f "${tmp}"
    if [ "${DOWNLOADER}" = "curl" ]; then
        if [ "${quiet}" = "quiet" ]; then
            curl -fsSL "${url}" -o "${tmp}" 2>/dev/null || { rm -f "${tmp}"; return 1; }
        else
            curl -fL --progress-bar "${url}" -o "${tmp}" || { rm -f "${tmp}"; return 1; }
        fi
    else
        if [ "${quiet}" = "quiet" ]; then
            wget -q -O "${tmp}" "${url}" 2>/dev/null || { rm -f "${tmp}"; return 1; }
        else
            wget --progress=bar:force:noscroll -O "${tmp}" "${url}" 2>/dev/null || { rm -f "${tmp}"; return 1; }
        fi
    fi
    if [ ! -s "${tmp}" ]; then
        rm -f "${tmp}"
        return 1
    fi
    mv -f "${tmp}" "${dest}"
    chmod +x "${dest}" 2>/dev/null || true
    return 0
}

# ==============================================================================
# 3. DETECT UPDATE VS FRESH INSTALL
# ==============================================================================
IS_UPDATE=false
if [ -f "${INSTALL_DIR}/get-hike" ] || [ -f "${DB_PATH}" ]; then
    IS_UPDATE=true
    log "🔄 Existing installation detected — running as UPDATE (data preserved)."
else
    log "🆕 Fresh installation."
fi

# ==============================================================================
# 4. STOP RUNNING INSTANCES (before touching the database)
# ==============================================================================
log "Stopping any running ${APP_NAME} processes..."

if command -v systemctl >/dev/null 2>&1; then
    sudo_run systemctl --user stop "${APP_NAME_LOWER}.service" 2>/dev/null || true
    sudo_run systemctl --user stop "mini-tracker.service" 2>/dev/null || true
fi

# Gracefully stop the detached background tracker daemon via its pidfile.
if [ -f "${PID_FILE}" ]; then
    daemon_pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [ -n "${daemon_pid:-}" ] && kill -0 "${daemon_pid}" 2>/dev/null; then
        kill -TERM "${daemon_pid}" 2>/dev/null || true
        i=0
        while [ "${i}" -lt 30 ] && kill -0 "${daemon_pid}" 2>/dev/null; do
            i=$((i + 1))
            sleep 0.1
        done
        if kill -0 "${daemon_pid}" 2>/dev/null; then
            kill -9 "${daemon_pid}" 2>/dev/null || true
        fi
        log "✅ Stopped background tracker daemon (pid ${daemon_pid})."
    fi
    rm -f "${PID_FILE}"
fi

# Safely terminate remaining app processes without killing this installer.
if command -v pgrep >/dev/null 2>&1; then
    for pid in $(pgrep -f "${APP_NAME_LOWER}|mini-tracker" 2>/dev/null || true); do
        [ "${pid}" = "$$" ] && continue
        [ "${pid}" = "${PPID}" ] && continue
        cmdline=""
        if [ -r "/proc/${pid}/cmdline" ]; then
            cmdline="$(tr '\0' ' ' < "/proc/${pid}/cmdline" 2>/dev/null || true)"
        elif command -v ps >/dev/null 2>&1; then
            cmdline="$(ps -o args= -p "${pid}" 2>/dev/null || true)"
        fi
        case "${cmdline}" in
            *install.sh*) continue ;; # covers uninstall.sh as well
        esac
        kill -TERM "${pid}" 2>/dev/null || true
        sleep 0.2
        if kill -0 "${pid}" 2>/dev/null; then
            kill -9 "${pid}" 2>/dev/null || true
        fi
    done
fi

# ==============================================================================
# 5. PRESERVE USER DATA (DB + WAL/SHM) AFTER PROCESSES ARE STOPPED
# ==============================================================================
# The database stores all user logs, screenshots and AI analytics. It must
# never be deleted or overwritten during an update.
DB_BACKUP_PATH=""
if [ "${IS_UPDATE}" = true ] && [ -f "${DB_PATH}" ]; then
    DB_BACKUP_PATH="${DB_PATH}.bak_$(date +%Y%m%d%H%M%S)"
    if command -v sqlite3 >/dev/null 2>&1 &&
        sqlite3 "${DB_PATH}" ".backup '${DB_BACKUP_PATH}'" 2>/dev/null; then
        log "✅ Database backed up via sqlite3 .backup: ${DB_BACKUP_PATH}"
    else
        cp -a "${DB_PATH}" "${DB_BACKUP_PATH}"
        for suffix in -wal -shm; do
            if [ -f "${DB_PATH}${suffix}" ]; then
                cp -a "${DB_PATH}${suffix}" "${DB_BACKUP_PATH}${suffix}" 2>/dev/null || true
            fi
        done
        log "✅ Database backed up to: ${DB_BACKUP_PATH}"
    fi
fi

# ==============================================================================
# 6. RETRIEVE OR BUILD APPLICATION BINARY
# ==============================================================================
BINARY_SOURCE=""
CUSTOM_BUILD_PATH="${LOCAL_BUILD_PATH:-${POSITIONAL_ARG}}"

DEFAULT_REPO_URL="https://github.com/sandeshPatel06/mini-tracker"

LATEST_TAG=""
if command -v gh >/dev/null 2>&1; then
    LATEST_TAG="$(gh release view --json tagName -q .tagName 2>/dev/null || echo "")"
fi

if [ -n "${LATEST_TAG}" ]; then
    DEFAULT_DOWNLOAD_URL="${DEFAULT_REPO_URL}/releases/download/${LATEST_TAG}/mini-tracker"
else
    DEFAULT_DOWNLOAD_URL="${DEFAULT_REPO_URL}/releases/latest/download/mini-tracker"
fi

EFFECTIVE_DOWNLOAD_URL="${DOWNLOAD_URL:-${DEFAULT_DOWNLOAD_URL}}"

is_local_binary() {
    [ -f "$1" ] && [ -s "$1" ]
}

# Priority 1: explicit local path / URL argument.
if [ -n "${CUSTOM_BUILD_PATH}" ]; then
    if [[ "${CUSTOM_BUILD_PATH}" =~ ^https?:// ]]; then
        log "📥 Downloading binary from specified URL: ${CUSTOM_BUILD_PATH}..."
        if download_file "${CUSTOM_BUILD_PATH}" "bin/mini-tracker"; then
            BINARY_SOURCE="bin/mini-tracker"
            log "✅ Downloaded binary to: ${BINARY_SOURCE}"
        else
            log "❌ Failed to download binary from URL: ${CUSTOM_BUILD_PATH}"
            exit 1
        fi
    elif is_local_binary "${CUSTOM_BUILD_PATH}"; then
        BINARY_SOURCE="${CUSTOM_BUILD_PATH}"
        chmod +x "${BINARY_SOURCE}" 2>/dev/null || true
        log "✅ Using specified custom local build binary: ${BINARY_SOURCE}"
    else
        log "⚠️ Specified path '${CUSTOM_BUILD_PATH}' not usable (missing/not executable). Attempting remote download..."
    fi
fi

# Priority 2: GitHub release download.
if [ -z "${BINARY_SOURCE}" ]; then
    log "📥 Attempting download from GitHub release: ${EFFECTIVE_DOWNLOAD_URL}..."
    if download_file "${EFFECTIVE_DOWNLOAD_URL}" "bin/mini-tracker"; then
        BINARY_SOURCE="bin/mini-tracker"
        log "✅ Downloaded release binary from GitHub to: ${BINARY_SOURCE}"
    else
        log "ℹ GitHub release binary not available — searching local workspace..."
    fi
fi

# Priority 3: local pre-built binaries, then build from source.
if [ -z "${BINARY_SOURCE}" ]; then
    for candidate in "./mini-tracker" "bin/mini-tracker" "build/bin/mini-tracker" "build/bin/mini-tracker-tmp"; do
        if is_local_binary "${candidate}"; then
            BINARY_SOURCE="${candidate}"
            chmod +x "${BINARY_SOURCE}" 2>/dev/null || true
            log "✅ Found local application binary: ${BINARY_SOURCE}"
            break
        fi
    done
fi

if [ -z "${BINARY_SOURCE}" ]; then
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

# ==============================================================================
# 7. COPY & INSTALL APPLICATION FILES
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
    cp -r frontend/dist/. "${INSTALL_DIR}/frontend/dist/" 2>/dev/null || true
fi

# Symlinks for the desktop/CLI binaries.
ln -sf "${INSTALL_DIR}/get-hike" "${BIN_DIR}/get-hike"
ln -sf "${INSTALL_DIR}/mini-tracker" "${BIN_DIR}/mini-tracker"

# ==============================================================================
# 8. GUI LAUNCHER SCRIPT SETUP
# ==============================================================================
GUI_LAUNCHER="${INSTALL_DIR}/get-hike-gui"
cat > "${GUI_LAUNCHER}" << EOF
#!/usr/bin/env bash
exec "${INSTALL_DIR}/get-hike" "\$@"
EOF
chmod 755 "${GUI_LAUNCHER}"

ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/get-hike-gui"
ln -sf "${GUI_LAUNCHER}" "${BIN_DIR}/mini-tracker-gui"
log "✅ Installed desktop application launchers."

# ==============================================================================
# 9. PATH CONFIGURATION FOR USER SPACE
# ==============================================================================
if [ "${IS_SUDO}" = false ]; then
    SHELL_RC=""
    for rc in "${REAL_HOME}/.zshrc" "${REAL_HOME}/.bashrc" "${REAL_HOME}/.profile"; do
        if [ -f "${rc}" ]; then
            SHELL_RC="${rc}"
            break
        fi
    done

    if [ -n "${SHELL_RC}" ]; then
        if ! grep -q 'PATH.*\.local/bin' "${SHELL_RC}" 2>/dev/null; then
            {
                echo ''
                echo '# get-Hike PATH'
                echo 'export PATH="$HOME/.local/bin:$PATH"'
            } >> "${SHELL_RC}"
            log "✅ Added ${BIN_DIR} to PATH in ${SHELL_RC}"
        fi
    else
        log "ℹ No shell rc file found — add ${BIN_DIR} to your PATH manually."
    fi
fi

# ==============================================================================
# 10. CONFIGURATION & DATA DIRECTORIES
# ==============================================================================
log "Provisioning directories..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${DATA_DIR}/images"
mkdir -p "${DATA_DIR}/logs"

# Data preservation guard: restore from backup if the DB vanished mid-install.
if [ -f "${DB_PATH}" ]; then
    log "✅ User database preserved: ${DB_PATH} ($(human_size "${DB_PATH}") on disk)"
elif [ -n "${DB_BACKUP_PATH}" ] && [ -f "${DB_BACKUP_PATH}" ] && [ ! -f "${DB_PATH}" ]; then
    cp -a "${DB_BACKUP_PATH}" "${DB_PATH}"
    log "⚠️ Database restored from backup: ${DB_BACKUP_PATH}"
fi

# ==============================================================================
# 11. DESKTOP SHORTCUTS & APP ICON INTEGRATION
# ==============================================================================
log "Creating desktop launcher shortcuts & app icons..."
mkdir -p "${APPLICATIONS_DIR}"
mkdir -p "${AUTOSTART_DIR}"
mkdir -p "${ICON_DIR}"
mkdir -p "${PIXMAP_DIR}"

# Clean up legacy/alias desktop launchers and duplicate icons.
rm -f "${APPLICATIONS_DIR}/mini-tracker.desktop" \
      "${APPLICATIONS_DIR}/mini-tracker-server.desktop" \
      "${APPLICATIONS_DIR}/mini-tracker-tmp.desktop" \
      "${APPLICATIONS_DIR}/localhost.desktop" 2>/dev/null || true
find "${APPLICATIONS_DIR}" -maxdepth 1 -name 'get-Hike (*.desktop' -delete 2>/dev/null || true
find "${APPLICATIONS_DIR}" -maxdepth 1 -name 'get-hike (*.desktop' -delete 2>/dev/null || true

rm -f "${ICON_DIR}/mini-tracker.png" "${ICON_DIR}/mini-tracker-server.png" \
      "${ICON_DIR}/mini-tracker-tmp.png" "${ICON_DIR}/localhost.png" 2>/dev/null || true
rm -f "${PIXMAP_DIR}/mini-tracker.png" "${PIXMAP_DIR}/mini-tracker-server.png" \
      "${PIXMAP_DIR}/mini-tracker-tmp.png" "${PIXMAP_DIR}/localhost.png" 2>/dev/null || true

# Resolve the application icon from the repo, or download a fallback copy.
ICON_SOURCE=""
for candidate in \
    "${SCRIPT_DIR}/frontend/src/assets/logo.png" \
    "frontend/src/assets/logo.png" \
    "${SCRIPT_DIR}/frontend/src/assets/images/logo-universal.png" \
    "frontend/src/assets/images/logo-universal.png" \
    "${SCRIPT_DIR}/build/appicon.png" \
    "build/appicon.png"; do
    if [ -f "${candidate}" ]; then
        ICON_SOURCE="${candidate}"
        break
    fi
done

INSTALLED_ICON_PATH="${INSTALL_DIR}/get-hike.png"

if [ -z "${ICON_SOURCE}" ]; then
    log "📥 Standalone execution detected — downloading app icon from repository..."
    if download_file \
        "https://raw.githubusercontent.com/sandeshPatel06/mini-tracker/main/frontend/src/assets/logo.png" \
        "${INSTALLED_ICON_PATH}" quiet; then
        ICON_SOURCE="${INSTALLED_ICON_PATH}"
        log "✅ Downloaded remote app icon to ${INSTALLED_ICON_PATH}"
    fi
fi

if [ -n "${ICON_SOURCE}" ] && [ -f "${ICON_SOURCE}" ]; then
    cp -f "${ICON_SOURCE}" "${INSTALLED_ICON_PATH}"
    cp -f "${ICON_SOURCE}" "${INSTALL_DIR}/logo.png"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/get-hike.png"
    cp -f "${ICON_SOURCE}" "${ICON_DIR}/mini-tracker.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/get-hike.png"
    cp -f "${ICON_SOURCE}" "${PIXMAP_DIR}/mini-tracker.png"
    chmod 644 "${INSTALLED_ICON_PATH}" "${INSTALL_DIR}/logo.png" \
        "${ICON_DIR}/get-hike.png" "${PIXMAP_DIR}/get-hike.png" 2>/dev/null || true
    log "✅ Installed app icon to ${INSTALLED_ICON_PATH} and system icon themes."
else
    log "⚠️ Warning: App icon source could not be resolved — launchers will use a default icon."
fi

# Primary .desktop entry.
DESKTOP_ENTRY_FILE="${APPLICATIONS_DIR}/get-hike.desktop"
cat > "${DESKTOP_ENTRY_FILE}" << EOF
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
chmod 644 "${DESKTOP_ENTRY_FILE}"
gio set "${DESKTOP_ENTRY_FILE}" metadata::trusted true 2>/dev/null || true

AUTOSTART_FILE="${AUTOSTART_DIR}/get-hike.desktop"
cp -f "${DESKTOP_ENTRY_FILE}" "${AUTOSTART_FILE}"
chmod 644 "${AUTOSTART_FILE}"

# Also install launcher to the user's Desktop folder if present.
USER_DESKTOP_DIR="${REAL_HOME}/Desktop"
if [ -d "${USER_DESKTOP_DIR}" ]; then
    cp -f "${DESKTOP_ENTRY_FILE}" "${USER_DESKTOP_DIR}/get-hike.desktop"
    chmod 644 "${USER_DESKTOP_DIR}/get-hike.desktop"
    gio set "${USER_DESKTOP_DIR}/get-hike.desktop" metadata::trusted true 2>/dev/null || true
    if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
        chown "${REAL_USER}:${REAL_GROUP}" "${USER_DESKTOP_DIR}/get-hike.desktop" 2>/dev/null || true
    fi
    log "✅ Installed desktop launcher to ${USER_DESKTOP_DIR}/get-hike.desktop"
fi

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${APPLICATIONS_DIR}" 2>/dev/null || true
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1 && [ -d "${ICON_THEME_DIR}" ]; then
    gtk-update-icon-cache -f -t "${ICON_THEME_DIR}" 2>/dev/null || true
fi
log "✅ Created Desktop & Autostart launchers with custom icon."

# ==============================================================================
# 12. LEGACY SYSTEMD SERVICE CLEANUP
# ==============================================================================
log "Configuring desktop client autostart..."
if command -v systemctl >/dev/null 2>&1; then
    sudo_run systemctl --user disable --now "${APP_NAME_LOWER}.service" 2>/dev/null || true
    sudo_run systemctl --user disable --now "mini-tracker.service" 2>/dev/null || true
    rm -f "${SERVICE_FILE}"
    rm -f "${SYSTEMD_USER_DIR}/mini-tracker.service"
fi

# ==============================================================================
# 13. OWNERSHIP, HARDWARE PERMISSIONS & ZERO-SUDO SETUP
# ==============================================================================
if [ "${IS_SUDO}" = true ] && [ "${REAL_USER}" != "root" ]; then
    chown -R "${REAL_USER}:${REAL_GROUP}" \
        "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" 2>/dev/null || true
    chown -R "${REAL_USER}:${REAL_GROUP}" "${ICON_DIR}" "${PIXMAP_DIR}" 2>/dev/null || true
    [ -f "${AUTOSTART_FILE}" ] && chown "${REAL_USER}:${REAL_GROUP}" "${AUTOSTART_FILE}" 2>/dev/null || true
    [ -f "${DESKTOP_ENTRY_FILE}" ] && chown "${REAL_USER}:${REAL_GROUP}" "${DESKTOP_ENTRY_FILE}" 2>/dev/null || true
    if [ "${APPLICATIONS_DIR}" = "${REAL_HOME}/.local/share/applications" ]; then
        chown -R "${REAL_USER}:${REAL_GROUP}" "${APPLICATIONS_DIR}" 2>/dev/null || true
    fi

    # Zero-logout udev rule for instant hardware input access.
    UDEV_RULE_SRC="${SCRIPT_DIR}/scripts/99-get-hike-input.rules"
    if [ ! -f "${UDEV_RULE_SRC}" ]; then
        UDEV_RULE_SRC="${SCRIPT_DIR}/scripts/99-mini-tracker-input.rules"
    fi
    if [ -f "${UDEV_RULE_SRC}" ] && [ -d "/etc/udev/rules.d" ]; then
        cp -f "${UDEV_RULE_SRC}" "/etc/udev/rules.d/99-get-hike-input.rules" 2>/dev/null || true
        command -v udevadm >/dev/null 2>&1 && { udevadm control --reload-rules 2>/dev/null || true; udevadm trigger 2>/dev/null || true; }
        log "✅ Installed udev rule for zero-logout hardware input access."
    fi

    # File capabilities as an additional zero-group option.
    if command -v setcap >/dev/null 2>&1 && [ -f "${INSTALL_DIR}/get-hike" ]; then
        setcap cap_dac_read_search+ep "${INSTALL_DIR}/get-hike" 2>/dev/null || true
    fi
else
    log "ℹ️ Running as user without sudo: Zero-Sudo Application & API input tracking enabled automatically."
fi

# ==============================================================================
# 14. VERIFY INSTALLATION
# ==============================================================================
if [ -f "${INSTALL_DIR}/get-hike" ] && [ -x "${INSTALL_DIR}/get-hike" ]; then
    if [ "${IS_UPDATE}" = true ]; then
        log "🔄 ${APP_NAME} updated successfully — all your logs and history are preserved."
    else
        log "🎉 ${APP_NAME} desktop application installed successfully."
    fi
    log "   Binary : ${INSTALL_DIR}/get-hike"
    log "   Data   : ${DATA_DIR}  (logs, screenshots, tracker.db)"
    log "   Config : ${CONFIG_DIR}"
    log "   Log    : ${LOG_FILE}"
    if [ -f "${DB_PATH}" ]; then
        log "   DB     : ${DB_PATH} ($(human_size "${DB_PATH}"))"
    fi
    if [ -n "${DB_BACKUP_PATH}" ] && [ -f "${DB_BACKUP_PATH}" ]; then
        log "   Backup : ${DB_BACKUP_PATH} (safe to delete after verifying the app works)"
    fi
else
    log "❌ Installation failed."
    if [ -n "${DB_BACKUP_PATH}" ] && [ -f "${DB_BACKUP_PATH}" ] && [ ! -f "${DB_PATH}" ]; then
        cp -a "${DB_BACKUP_PATH}" "${DB_PATH}"
        log "⚠️  Restored database from backup: ${DB_BACKUP_PATH}"
    fi
    exit 3
fi

exit 0
