#!/usr/bin/env bash
set -e

# ==============================================================================
# Mini Tracker — Desktop Linux Installer
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
echo "        📍 Mini Tracker — Desktop Linux Installer                "
echo "================================================================="
echo -e "${NC}"

INSTALL_BIN_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/mini-tracker"
DATA_DIR="${HOME}/.local/share/mini-tracker"
APPLICATIONS_DIR="${HOME}/.local/share/applications"
AUTOSTART_DIR="${HOME}/.config/autostart"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
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

# 3. Build Go Server Binary
info "Building zero-dependency Go application binary..."
mkdir -p bin
go build -o bin/mini-tracker-server ./cmd/server || error "Failed to compile Go application binary."
success "Application binary compiled: bin/mini-tracker-server"

# 4. Install Binary to User Path
info "Installing binary to ${INSTALL_BIN_DIR}..."
mkdir -p "${INSTALL_BIN_DIR}"
if systemctl --user is-active --quiet mini-tracker.service 2>/dev/null; then
    systemctl --user stop mini-tracker.service 2>/dev/null || true
fi
install -m 755 bin/mini-tracker-server "${INSTALL_BIN_DIR}/mini-tracker-server"
ln -sf "${INSTALL_BIN_DIR}/mini-tracker-server" "${INSTALL_BIN_DIR}/mini-tracker"
success "Installed mini-tracker-server & symlink 'mini-tracker' in ${INSTALL_BIN_DIR}"

# 5. Ensure PATH includes ~/.local/bin
SHELL_RC=""
if [ -f "${HOME}/.zshrc" ]; then
    SHELL_RC="${HOME}/.zshrc"
elif [ -f "${HOME}/.bashrc" ]; then
    SHELL_RC="${HOME}/.bashrc"
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

# 6. Initialize Config, Storage, and Environment Files
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

# 7. Create Desktop Application Launcher Entry (.desktop file)
info "Creating Linux Desktop Application shortcut..."
mkdir -p "${APPLICATIONS_DIR}"
mkdir -p "${AUTOSTART_DIR}"

DESKTOP_ENTRY_FILE="${APPLICATIONS_DIR}/mini-tracker.desktop"
cat << EOF > "${DESKTOP_ENTRY_FILE}"
[Desktop Entry]
Type=Application
Name=Mini Tracker
GenericName=Productivity Tracker
Comment=Privacy-first Linux Productivity Tracker & AI Analyzer
Exec=sh -c "${INSTALL_BIN_DIR}/mini-tracker-server & sleep 1 && xdg-open ${BACKEND_ENDPOINT}"
Icon=utilities-system-monitor
Terminal=false
Categories=Utility;Development;Office;
Keywords=productivity;tracker;time;analytics;
StartupNotify=true
EOF
chmod +x "${DESKTOP_ENTRY_FILE}"
success "Installed Desktop Launcher: ${DESKTOP_ENTRY_FILE}"

# Autostart entry for login
AUTOSTART_FILE="${AUTOSTART_DIR}/mini-tracker.desktop"
cp "${DESKTOP_ENTRY_FILE}" "${AUTOSTART_FILE}"
success "Installed Desktop Autostart entry: ${AUTOSTART_FILE}"

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${APPLICATIONS_DIR}" 2>/dev/null || true
fi

# 8. Setup Systemd Background User Service
info "Setting up systemd background user daemon..."
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

if command -v systemctl >/dev/null 2>&1; then
    info "Enabling & starting mini-tracker background service..."
    systemctl --user daemon-reload
    systemctl --user enable --now mini-tracker.service || warn "Could not start systemd service automatically."
    success "Systemd background service is ACTIVE."
else
    warn "systemctl not found. You can launch the app from your application menu or run: mini-tracker"
fi

# 9. Check Keystroke Group Permissions
info "Checking keyboard entropy tracking permissions..."
if groups "$USER" | grep -q '\binput\b'; then
    success "User '$USER' is in 'input' group. Keystroke entropy capture is active."
else
    warn "User '$USER' is not in the 'input' group."
    warn "To enable hardware keystroke entropy tracking, run:"
    echo -e "  ${BOLD}sudo usermod -aG input \$USER${NC}"
    warn "(Requires logging out and back in after running)."
fi

# Final Summary
echo ""
echo -e "${BOLD}${GREEN}=================================================================${NC}"
echo -e "${BOLD}${GREEN} 🎉 Mini Tracker Desktop Installation Complete!                  ${NC}"
echo -e "${BOLD}${GREEN}=================================================================${NC}"
echo ""
echo -e "  • Desktop Launcher: Application Menu -> ${BOLD}Mini Tracker${NC}"
echo -e "  • Backend Endpoint: ${BOLD}${BLUE}${BACKEND_ENDPOINT}${NC}"
echo -e "  • Config Directory: ${CONFIG_DIR}"
echo -e "  • Storage Directory: ${DATA_DIR}"
echo -e "  • Binary Installed: ${INSTALL_BIN_DIR}/mini-tracker-server"
echo ""
echo -e "Management Commands:"
echo -e "  • Check service:   ${BOLD}systemctl --user status mini-tracker${NC}"
echo -e "  • View logs:       ${BOLD}journalctl --user -u mini-tracker -f${NC}"
echo -e "  • Stop daemon:    ${BOLD}systemctl --user stop mini-tracker${NC}"
echo -e "  • Launch App:      Search ${BOLD}'Mini Tracker'${NC} in your desktop menu"
echo ""
