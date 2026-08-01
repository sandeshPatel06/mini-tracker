#!/usr/bin/env bash
set -e

# ==============================================================================
# Mini Tracker — Universal Linux Desktop Installer (Sudo & Non-Sudo Support)
# ==============================================================================

BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[34m"
RED="\033[31m"
NC="\033[0m"

info()    { echo -e "${BLUE}ℹ${NC} ${BOLD}$1${NC}"; }
success() { echo -e "${GREEN}✓${NC} ${BOLD}$1${NC}"; }
warn()    { echo -e "${YELLOW}⚠️${NC} $1"; }
error()   { echo -e "${RED}✖${NC} $1"; exit 1; }

echo -e "${BOLD}"
echo "================================================================="
echo "        📍 Mini Tracker — Universal Linux Desktop Installer       "
echo "================================================================="
echo -e "${NC}"

# Detect execution context (sudo vs non-sudo)
if [ -n "${SUDO_USER}" ] && [ "${SUDO_USER}" != "root" ]; then
    IS_SUDO=true
    REAL_USER="${SUDO_USER}"
    REAL_HOME=$(getent passwd "${SUDO_USER}" | cut -d: -f6)
    REAL_UID=$(id -u "${SUDO_USER}")
    REAL_GROUP=$(id -gn "${SUDO_USER}")
    info "Running with sudo/root privileges for target user: ${BOLD}${REAL_USER}${NC}"
else
    IS_SUDO=false
    REAL_USER="${USER}"
    REAL_HOME="${HOME}"
    REAL_UID=$(id -u)
    REAL_GROUP=$(id -gn "${USER}" 2>/dev/null || echo "${USER}")
    info "Running in user space for target user: ${BOLD}${REAL_USER}${NC}"
fi

# Define paths based on execution mode
if [ "${IS_SUDO}" = true ]; then
    INSTALL_BIN_DIR="/usr/local/bin"
    APPLICATIONS_DIR="/usr/share/applications"
else
    INSTALL_BIN_DIR="${REAL_HOME}/.local/bin"
    APPLICATIONS_DIR="${REAL_HOME}/.local/share/applications"
fi

CONFIG_DIR="${REAL_HOME}/.config/mini-tracker"
DATA_DIR="${REAL_HOME}/.local/share/mini-tracker"
AUTOSTART_DIR="${REAL_HOME}/.config/autostart"
SYSTEMD_USER_DIR="${REAL_HOME}/.config/systemd/user"
SERVICE_FILE="${SYSTEMD_USER_DIR}/mini-tracker.service"

BACKEND_PORT="${PORT:-8080}"
BACKEND_ENDPOINT="${BACKEND_ENDPOINT:-http://localhost:${BACKEND_PORT}}"

# 1. Check prerequisites
info "Checking system prerequisites..."
command -v go >/dev/null 2>&1 || error "Go 1.22+ is required but not found. Please install Go: https://go.dev/doc/install"
command -v npm >/dev/null 2>&1 || error "Node.js & npm are required to build the frontend dashboard but not found."

GO_VERSION=$(go version | awk '{print $3}')
success "Found Go (${GO_VERSION})"
success "Found npm ($(npm -v))"

# 2. Build Frontend
info "Building React Web Dashboard..."
if [ -d "frontend" ]; then
    (cd frontend && npm install --silent && npm run build) || error "Failed to build frontend dashboard."
    success "Frontend compiled successfully."
else
    error "Frontend directory not found. Please run install.sh from the root of mini-tracker repository."
fi

# 3. Build Go Application Binary
info "Building zero-dependency Go application binary..."
mkdir -p bin
go build -o bin/mini-tracker-server ./cmd/server || error "Failed to compile Go application binary."
success "Application binary compiled: bin/mini-tracker-server"

# 4. Install Binary & GUI App Launcher Script
info "Installing application binaries to ${INSTALL_BIN_DIR}..."
mkdir -p "${INSTALL_BIN_DIR}"

# Stop systemd user service if active before replacing binary
if [ "${IS_SUDO}" = true ]; then
    sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user stop mini-tracker.service 2>/dev/null || true
else
    systemctl --user stop mini-tracker.service 2>/dev/null || true
fi

install -m 755 bin/mini-tracker-server "${INSTALL_BIN_DIR}/mini-tracker-server"
ln -sf "${INSTALL_BIN_DIR}/mini-tracker-server" "${INSTALL_BIN_DIR}/mini-tracker"

# Create Desktop App Launcher script (mini-tracker-gui)
GUI_LAUNCHER="${INSTALL_BIN_DIR}/mini-tracker-gui"
cat << 'EOF' > "${GUI_LAUNCHER}"
#!/usr/bin/env bash
# Load environment config if available
ENV_PATH="$HOME/.config/mini-tracker/.env"
if [ -f "$ENV_PATH" ]; then
    set -a
    source "$ENV_PATH"
    set +a
fi

ENDPOINT="${BACKEND_ENDPOINT:-http://localhost:8080}"

# Ensure mini-tracker-server is running
if ! pgrep -f "mini-tracker-server" >/dev/null 2>&1; then
    if command -v mini-tracker-server >/dev/null 2>&1; then
        mini-tracker-server &
    elif [ -f "$HOME/.local/bin/mini-tracker-server" ]; then
        "$HOME/.local/bin/mini-tracker-server" &
    elif [ -f "/usr/local/bin/mini-tracker-server" ]; then
        "/usr/local/bin/mini-tracker-server" &
    fi
    sleep 1
fi

# Launch standalone Desktop App window in app mode
if command -v google-chrome >/dev/null 2>&1; then
    exec google-chrome --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
elif command -v google-chrome-stable >/dev/null 2>&1; then
    exec google-chrome-stable --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
elif command -v chromium >/dev/null 2>&1; then
    exec chromium --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
elif command -v chromium-browser >/dev/null 2>&1; then
    exec chromium-browser --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
elif command -v brave-browser >/dev/null 2>&1; then
    exec brave-browser --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
elif command -v microsoft-edge >/dev/null 2>&1; then
    exec microsoft-edge --app="${ENDPOINT}" --class="mini-tracker" --name="Mini Tracker" "$@"
else
    exec xdg-open "${ENDPOINT}"
fi
EOF
chmod +x "${GUI_LAUNCHER}"
success "Installed binaries: mini-tracker-server, mini-tracker, and mini-tracker-gui"

# 5. Ensure PATH includes ~/.local/bin if installed in user space
if [ "${IS_SUDO}" = false ]; then
    SHELL_RC=""
    if [ -f "${REAL_HOME}/.zshrc" ]; then
        SHELL_RC="${REAL_HOME}/.zshrc"
    elif [ -f "${REAL_HOME}/.bashrc" ]; then
        SHELL_RC="${REAL_HOME}/.bashrc"
    fi

    if [ -n "${SHELL_RC}" ]; then
        if ! grep -q 'PATH.*\.local/bin' "${SHELL_RC}"; then
            info "Adding ${INSTALL_BIN_DIR} to PATH in ${SHELL_RC}..."
            echo '' >> "${SHELL_RC}"
            echo '# Mini Tracker PATH' >> "${SHELL_RC}"
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "${SHELL_RC}"
            success "Updated ${SHELL_RC}"
        fi
    fi
fi

# 6. Initialize Config & Data Directories
info "Initializing configuration, environment, and storage..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${DATA_DIR}"

ENV_FILE="${CONFIG_DIR}/.env"
if [ ! -f "${ENV_FILE}" ]; then
    cat << EOF > "${ENV_FILE}"
# Mini Tracker Environment Configuration
GEMINI_API_KEY=${GEMINI_API_KEY:-}
SCREENSHOT_INTERVAL_SECONDS=30
AI_ANALYSIS_INTERVAL=3h
PORT=${BACKEND_PORT}
BACKEND_ENDPOINT=${BACKEND_ENDPOINT}
EOF
    success "Created environment config file: ${ENV_FILE}"
else
    success "Preserved existing environment file: ${ENV_FILE}"
fi

CONFIG_FILE="${CONFIG_DIR}/config.json"
if [ ! -f "${CONFIG_FILE}" ]; then
    cat << EOF > "${CONFIG_FILE}"
{
  "gemini_api_key": "${GEMINI_API_KEY:-}",
  "ai_analysis_interval_seconds": 10800,
  "screenshot_interval_seconds": 30,
  "backend_port": ${BACKEND_PORT},
  "backend_endpoint": "${BACKEND_ENDPOINT}",
  "data_dir": "${DATA_DIR}"
}
EOF
    success "Created configuration file: ${CONFIG_FILE}"
else
    success "Preserved existing configuration: ${CONFIG_FILE}"
fi

if [ "${IS_SUDO}" = true ]; then
    chown -R "${REAL_USER}:${REAL_GROUP}" "${CONFIG_DIR}" "${DATA_DIR}"
fi

# 7. Create Standalone Linux Desktop Application Entry (.desktop file)
info "Creating Linux Desktop Application shortcut..."
mkdir -p "${APPLICATIONS_DIR}"
mkdir -p "${AUTOSTART_DIR}"

DESKTOP_ENTRY_FILE="${APPLICATIONS_DIR}/mini-tracker.desktop"
cat << EOF > "${DESKTOP_ENTRY_FILE}"
[Desktop Entry]
Type=Application
Name=Mini Tracker
GenericName=Productivity Tracker Desktop App
Comment=Privacy-first Linux Productivity Tracker & AI Analyzer
Exec=${GUI_LAUNCHER}
Icon=utilities-system-monitor
Terminal=false
Categories=Utility;Development;Office;
Keywords=productivity;tracker;time;analytics;
StartupNotify=true
StartupWMClass=mini-tracker
EOF
chmod +x "${DESKTOP_ENTRY_FILE}"
success "Installed Desktop Launcher: ${DESKTOP_ENTRY_FILE}"

# Autostart entry for login
AUTOSTART_FILE="${AUTOSTART_DIR}/mini-tracker.desktop"
cp "${DESKTOP_ENTRY_FILE}" "${AUTOSTART_FILE}"
success "Installed Desktop Autostart entry: ${AUTOSTART_FILE}"

if [ "${IS_SUDO}" = true ]; then
    chown "${REAL_USER}:${REAL_GROUP}" "${AUTOSTART_FILE}"
    if [ "${APPLICATIONS_DIR}" = "${REAL_HOME}/.local/share/applications" ]; then
        chown -R "${REAL_USER}:${REAL_GROUP}" "${APPLICATIONS_DIR}"
    fi
fi

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${APPLICATIONS_DIR}" 2>/dev/null || true
fi

# 8. Setup Systemd Background User Service
info "Setting up systemd background user daemon for ${REAL_USER}..."
mkdir -p "${SYSTEMD_USER_DIR}"

cat << EOF > "${SERVICE_FILE}"
[Unit]
Description=Mini Tracker Productivity Daemon
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_BIN_DIR}/mini-tracker-server
Restart=always
RestartSec=5s
Environment="PORT=${BACKEND_PORT}"
Environment="BACKEND_ENDPOINT=${BACKEND_ENDPOINT}"
Environment="DATA_DIR=${DATA_DIR}"
EnvironmentFile=-${ENV_FILE}

[Install]
WantedBy=default.target
EOF
success "Created systemd service file: ${SERVICE_FILE}"

if [ "${IS_SUDO}" = true ]; then
    chown -R "${REAL_USER}:${REAL_GROUP}" "${SYSTEMD_USER_DIR}"
fi

# Enable systemd service
if command -v systemctl >/dev/null 2>&1; then
    info "Enabling & starting mini-tracker background service..."
    if [ "${IS_SUDO}" = true ]; then
        sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user daemon-reload 2>/dev/null || true
        sudo -u "${REAL_USER}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${REAL_UID}/bus" systemctl --user enable --now mini-tracker.service 2>/dev/null || warn "Systemd user service enabled for ${REAL_USER}."
    else
        systemctl --user daemon-reload
        systemctl --user enable --now mini-tracker.service || warn "Systemd user service enabled."
    fi
    success "Systemd background service is configured & active."
fi

# 9. Handle Keystroke Group Permissions
info "Checking keyboard entropy tracking permissions..."
if groups "${REAL_USER}" | grep -q '\binput\b'; then
    success "User '${REAL_USER}' is in 'input' group. Keystroke entropy capture is active."
else
    if [ "${IS_SUDO}" = true ]; then
        info "Adding '${REAL_USER}' to the 'input' group automatically..."
        usermod -aG input "${REAL_USER}"
        success "Added '${REAL_USER}' to 'input' group. (Log out and log back in for group change to take effect)."
    else
        warn "User '${REAL_USER}' is not in the 'input' group."
        warn "To enable hardware keystroke entropy tracking, run:"
        echo -e "  ${BOLD}sudo usermod -aG input ${REAL_USER}${NC}"
        warn "(Requires logging out and back in after running)."
    fi
fi

# Final Summary
echo ""
echo -e "${BOLD}${GREEN}=================================================================${NC}"
echo -e "${BOLD}${GREEN} 🎉 Mini Tracker Desktop App Installation Complete!              ${NC}"
echo -e "${BOLD}${GREEN}=================================================================${NC}"
echo ""
echo -e "  • Target User:       ${BOLD}${REAL_USER}${NC}"
echo -e "  • Mode:              ${BOLD}$( [ "$IS_SUDO" = true ] && echo "Sudo System-Wide Install" || echo "User Space Install" )${NC}"
echo -e "  • Desktop App:       Application Menu -> ${BOLD}Mini Tracker${NC}"
echo -e "  • GUI Launcher:      ${GUI_LAUNCHER}"
echo -e "  • Backend Endpoint:  ${BOLD}${BLUE}${BACKEND_ENDPOINT}${NC}"
echo -e "  • Config Directory:  ${CONFIG_DIR}"
echo -e "  • Storage Directory: ${DATA_DIR}"
echo ""
echo -e "Management Commands:"
echo -e "  • Launch Desktop App: ${BOLD}mini-tracker-gui${NC} (or click desktop icon)"
echo -e "  • Check daemon:       ${BOLD}systemctl --user status mini-tracker${NC}"
echo -e "  • View logs:           ${BOLD}journalctl --user -u mini-tracker -f${NC}"
echo -e "  • Stop daemon:        ${BOLD}systemctl --user stop mini-tracker${NC}"
echo ""
