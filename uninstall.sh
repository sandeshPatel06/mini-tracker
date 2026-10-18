#!/usr/bin/env bash
#
# get-Hike uninstaller — removes launchers, binaries and (optionally) user data.
#
# Usage:
#   ./uninstall.sh                 # remove app + launchers + user data & config
#   ./uninstall.sh --keep-data     # remove app, keep logs/screenshots/tracker.db
#   ./uninstall.sh --help
#
set -euo pipefail

APP_NAME="get-Hike"
APP_NAME_LOWER="get-hike"

KEEP_DATA=false

usage() {
    cat <<'EOF'
get-Hike uninstaller — removes launchers, binaries and (optionally) user data.

Usage:
  ./uninstall.sh                 # remove app + launchers + user data & config
  ./uninstall.sh --keep-data     # remove app, keep logs/screenshots/tracker.db
  ./uninstall.sh --help
EOF
}

for arg in "$@"; do
    case "$arg" in
        -h|--help)
            usage
            exit 0
            ;;
        --keep-data)
            KEEP_DATA=true
            ;;
        *)
            echo "Unknown option: $arg (use --help)" >&2
            exit 2
            ;;
    esac
done

# ==============================================================================
# USER / SUDO CONTEXT
# ==============================================================================
if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
    REAL_USER="${SUDO_USER}"
    REAL_HOME="$(getent passwd "${REAL_USER}" 2>/dev/null | cut -d: -f6 || true)"
    if [ -z "${REAL_HOME}" ]; then
        REAL_HOME="$(eval echo "~${REAL_USER}" 2>/dev/null || echo "/home/${REAL_USER}")"
    fi
    REAL_UID="$(id -u "${REAL_USER}" 2>/dev/null || echo 0)"
else
    REAL_USER="$(id -un)"
    REAL_HOME="${HOME:-$(eval echo "~${REAL_USER}" 2>/dev/null || echo "~")}"
    REAL_UID="$(id -u)"
fi

DATA_DIR="${REAL_HOME}/.local/share/${APP_NAME_LOWER}"
CONFIG_DIR="${REAL_HOME}/.config/${APP_NAME_LOWER}"
DB_PATH="${DATA_DIR}/tracker.db"
PID_FILE="${DATA_DIR}/tracker.pid"

SUDO=""
if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
fi

# Run a user-level systemctl (as SUDO_USER when the script itself runs via sudo).
user_systemctl() {
    if [ "$(id -u)" -eq 0 ] && [ "${REAL_USER}" != "root" ]; then
        sudo -u "${REAL_USER}" \
            XDG_RUNTIME_DIR="/run/user/${REAL_UID}" \
            DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" \
            systemctl "$@"
    else
        systemctl "$@"
    fi
}

# Remove system-level paths (requires root or sudo; silently skipped otherwise).
rm_system() {
    for path in "$@"; do
        [ -e "${path}" ] || continue
        if [ "$(id -u)" -eq 0 ]; then
            rm -rf "${path}" 2>/dev/null || true
        elif [ -n "${SUDO}" ]; then
            ${SUDO} rm -rf "${path}" 2>/dev/null || true
        else
            echo "⚠️  Skipped (needs sudo): ${path}"
        fi
    done
}

echo "🗑️ Stopping and uninstalling ${APP_NAME} & legacy builds..."

# ==============================================================================
# 1. STOP BACKGROUND SERVICES AND RUNNING PROCESSES
# ==============================================================================
if command -v systemctl >/dev/null 2>&1; then
    for unit in "${APP_NAME_LOWER}.service" "mini-tracker.service"; do
        user_systemctl --user stop "${unit}" 2>/dev/null || true
        user_systemctl --user disable "${unit}" 2>/dev/null || true
    done
    user_systemctl --user reset-failed 2>/dev/null || true
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
        echo "✅ Stopped background tracker daemon (pid ${daemon_pid})."
    fi
    rm -f "${PID_FILE}"
fi

# Terminate remaining app processes (exact names first — safest).
TERM_KILLED=false
for name in "get-hike" "mini-tracker"; do
    if pkill -TERM -x "${name}" 2>/dev/null; then
        TERM_KILLED=true
    fi
done
if [ "${TERM_KILLED}" = true ]; then
    sleep 0.5
fi
for name in "get-hike" "mini-tracker"; do
    pkill -KILL -x "${name}" 2>/dev/null || true
done

# Fallback for renamed/aliased binaries, skipping this script and its parents.
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
# 2. REMOVE SYSTEMD USER UNITS & AUTOSTART
# ==============================================================================
rm -f "${REAL_HOME}/.config/systemd/user/get-hike.service" \
      "${REAL_HOME}/.config/systemd/user/mini-tracker.service" \
      "${REAL_HOME}/.config/systemd/user/default.target.wants/get-hike.service" \
      "${REAL_HOME}/.config/systemd/user/default.target.wants/mini-tracker.service" 2>/dev/null || true
if command -v systemctl >/dev/null 2>&1; then
    user_systemctl --user daemon-reload 2>/dev/null || true
fi

rm -f "${REAL_HOME}/.config/autostart/get-hike.desktop" \
      "${REAL_HOME}/.config/autostart/mini-tracker.desktop" 2>/dev/null || true

# ==============================================================================
# 3. REMOVE SYMLINKS & USER BINARIES
# ==============================================================================
rm -f "${REAL_HOME}/.local/bin/get-hike" \
      "${REAL_HOME}/.local/bin/get-hike-gui" \
      "${REAL_HOME}/.local/bin/mini-tracker" \
      "${REAL_HOME}/.local/bin/mini-tracker-server" \
      "${REAL_HOME}/.local/bin/mini-tracker-gui" 2>/dev/null || true

# ==============================================================================
# 4. REMOVE DESKTOP SHORTCUTS, ICONS & PIXMAPS (USER SPACE)
# ==============================================================================
rm -f "${REAL_HOME}/.local/share/applications/get-hike.desktop" \
      "${REAL_HOME}/.local/share/applications/mini-tracker.desktop" \
      "${REAL_HOME}/.local/share/applications/mini-tracker-server.desktop" \
      "${REAL_HOME}/.local/share/applications/mini-tracker-tmp.desktop" \
      "${REAL_HOME}/.local/share/applications/localhost.desktop" 2>/dev/null || true
find "${REAL_HOME}/.local/share/applications" -maxdepth 1 \
    \( -name 'get-Hike (*.desktop' -o -name 'get-hike (*.desktop' \) -delete 2>/dev/null || true

rm -f "${REAL_HOME}/Desktop/get-hike.desktop" "${REAL_HOME}/Desktop/mini-tracker.desktop" 2>/dev/null || true

rm -f "${REAL_HOME}/.local/share/icons/hicolor/512x512/apps/get-hike.png" \
      "${REAL_HOME}/.local/share/icons/hicolor/512x512/apps/get-hike"*.png \
      "${REAL_HOME}/.local/share/icons/hicolor/512x512/apps/mini-tracker"*.png \
      "${REAL_HOME}/.local/share/icons/hicolor/512x512/apps/localhost.png" 2>/dev/null || true
rm -f "${REAL_HOME}/.local/share/pixmaps/get-hike"*.png \
      "${REAL_HOME}/.local/share/pixmaps/mini-tracker"*.png \
      "${REAL_HOME}/.local/share/pixmaps/localhost.png" 2>/dev/null || true

# ==============================================================================
# 5. REMOVE SYSTEM-WIDE INSTALL (binaries, launchers, icons, udev rule)
# ==============================================================================
rm_system \
    "/opt/${APP_NAME_LOWER}" \
    "/opt/mini-tracker" \
    "/usr/local/bin/get-hike" \
    "/usr/local/bin/get-hike-gui" \
    "/usr/local/bin/mini-tracker" \
    "/usr/local/bin/mini-tracker-server" \
    "/usr/local/bin/mini-tracker-gui" \
    "/usr/share/applications/get-hike.desktop" \
    "/usr/share/applications/mini-tracker.desktop" \
    "/usr/share/applications/mini-tracker-server.desktop" \
    "/usr/share/applications/localhost.desktop" \
    "/usr/share/icons/hicolor/512x512/apps/get-hike.png" \
    "/usr/share/icons/hicolor/512x512/apps/mini-tracker.png" \
    "/usr/share/pixmaps/get-hike.png" \
    "/usr/share/pixmaps/mini-tracker.png" \
    "/etc/udev/rules.d/99-get-hike-input.rules" \
    "/etc/udev/rules.d/99-mini-tracker-input.rules" \
    "/var/log/get-hike_install.log"

if command -v udevadm >/dev/null 2>&1 && [ ! -f "/etc/udev/rules.d/99-get-hike-input.rules" ]; then
    if [ "$(id -u)" -eq 0 ] || [ -n "${SUDO}" ]; then
        if [ "$(id -u)" -eq 0 ]; then
            udevadm control --reload-rules 2>/dev/null || true
        else
            ${SUDO} udevadm control --reload-rules 2>/dev/null || true
        fi
    fi
fi

# ==============================================================================
# 6. REMOVE DATA & CONFIG DIRECTORIES (unless --keep-data)
# ==============================================================================
if [ "${KEEP_DATA}" = true ]; then
    echo "🛡️  --keep-data: preserving user data and configuration."
    [ -d "${DATA_DIR}" ] && echo "   Kept: ${DATA_DIR} (tracker.db, screenshots, logs)"
    [ -d "${CONFIG_DIR}" ] && echo "   Kept: ${CONFIG_DIR}"
else
    if [ -f "${DB_PATH}" ]; then
        echo "⚠️  Deleting user database: ${DB_PATH}"
    fi
    rm -rf "${DATA_DIR}" \
           "${REAL_HOME}/.local/share/mini-tracker" \
           "${REAL_HOME}/.local/share/mini-tracker-server" \
           "${REAL_HOME}/.local/share/mini-tracker-tmp"* \
           "${CONFIG_DIR}" \
           "${REAL_HOME}/.config/mini-tracker" 2>/dev/null || true
fi

# ==============================================================================
# 7. UPDATE CACHES
# ==============================================================================
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${REAL_HOME}/.local/share/applications" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "${REAL_HOME}/.local/share/icons/hicolor" 2>/dev/null || true
fi
if [ "$(id -u)" -eq 0 ] || [ -n "${SUDO}" ]; then
    if command -v update-desktop-database >/dev/null 2>&1 && [ -d "/usr/share/applications" ]; then
        if [ "$(id -u)" -eq 0 ]; then
            update-desktop-database "/usr/share/applications" 2>/dev/null || true
        else
            ${SUDO} update-desktop-database "/usr/share/applications" 2>/dev/null || true
        fi
    fi
fi

echo "✅ ${APP_NAME} & legacy builds have been removed."
if [ "${KEEP_DATA}" = true ]; then
    echo "   Reinstall later with ./install.sh — your data will be picked up automatically."
fi
