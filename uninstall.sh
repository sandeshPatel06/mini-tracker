#!/bin/bash
set -e

APP_NAME="Mini Tracker"
APP_NAME_LOWER="mini-tracker"

echo "🗑️ Stopping and uninstalling ${APP_NAME}..."

# 1. Stop background systemd service and processes
systemctl --user stop "${APP_NAME_LOWER}.service" 2>/dev/null || true
systemctl --user disable "${APP_NAME_LOWER}.service" 2>/dev/null || true

pkill -9 -x "${APP_NAME_LOWER}-server" 2>/dev/null || true
pkill -9 -x "${APP_NAME_LOWER}" 2>/dev/null || true
pkill -9 -x "${APP_NAME_LOWER}-gui" 2>/dev/null || true

# 2. Remove systemd user unit
rm -f "${HOME}/.config/systemd/user/${APP_NAME_LOWER}.service"
rm -f "${HOME}/.config/systemd/user/default.target.wants/${APP_NAME_LOWER}.service"
systemctl --user daemon-reload 2>/dev/null || true

# 3. Remove symlinks & binaries
rm -f "${HOME}/.local/bin/${APP_NAME_LOWER}"
rm -f "${HOME}/.local/bin/${APP_NAME_LOWER}-server"
rm -f "${HOME}/.local/bin/${APP_NAME_LOWER}-gui"

# 4. Remove desktop shortcuts & autostart launchers
rm -f "${HOME}/.local/share/applications/${APP_NAME_LOWER}.desktop"
rm -f "${HOME}/.local/share/applications/${APP_NAME_LOWER}-server.desktop"
rm -f "${HOME}/.local/share/applications/${APP_NAME_LOWER}-tmp.desktop"
rm -f "${HOME}/.local/share/applications/localhost.desktop"
rm -f "${HOME}/.config/autostart/${APP_NAME_LOWER}.desktop"

# 5. Remove icons & pixmaps
rm -f "${HOME}/.local/share/icons/hicolor/512x512/apps/${APP_NAME_LOWER}"*.png
rm -f "${HOME}/.local/share/icons/hicolor/512x512/apps/localhost.png"
rm -f "${HOME}/.local/share/pixmaps/${APP_NAME_LOWER}"*.png
rm -f "${HOME}/.local/share/pixmaps/localhost.png"

# 6. Remove data and config directories
rm -rf "${HOME}/.local/share/${APP_NAME_LOWER}"
rm -rf "${HOME}/.local/share/${APP_NAME_LOWER}-server"
rm -rf "${HOME}/.local/share/${APP_NAME_LOWER}-tmp"
rm -rf "${HOME}/.local/share/${APP_NAME_LOWER}-tmp-dev-linux-amd64"
rm -rf "${HOME}/.config/${APP_NAME_LOWER}"

# 7. Update caches
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${HOME}/.local/share/applications" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "${HOME}/.local/share/icons/hicolor" 2>/dev/null || true
fi

echo "✅ ${APP_NAME} has been completely uninstalled from your local system."
