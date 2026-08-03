#!/bin/bash
set -e

APP_NAME="get-Hike"
APP_NAME_LOWER="get-hike"

echo "🗑️ Stopping and completely uninstalling ${APP_NAME} & legacy builds..."

# 1. Stop background systemd services and processes
systemctl --user stop "get-hike.service" 2>/dev/null || true
systemctl --user disable "get-hike.service" 2>/dev/null || true
systemctl --user stop "mini-tracker.service" 2>/dev/null || true
systemctl --user disable "mini-tracker.service" 2>/dev/null || true

pkill -9 -f "get-hike" 2>/dev/null || true
pkill -9 -f "mini-tracker" 2>/dev/null || true

# 2. Remove systemd user units
rm -f "${HOME}/.config/systemd/user/get-hike.service"
rm -f "${HOME}/.config/systemd/user/mini-tracker.service"
rm -f "${HOME}/.config/systemd/user/default.target.wants/get-hike.service"
rm -f "${HOME}/.config/systemd/user/default.target.wants/mini-tracker.service"
systemctl --user daemon-reload 2>/dev/null || true

# 3. Remove symlinks & binaries
rm -f "${HOME}/.local/bin/get-hike"
rm -f "${HOME}/.local/bin/get-hike-gui"
rm -f "${HOME}/.local/bin/mini-tracker"
rm -f "${HOME}/.local/bin/mini-tracker-server"
rm -f "${HOME}/.local/bin/mini-tracker-gui"
rm -f "/usr/local/bin/get-hike" 2>/dev/null || true
rm -f "/usr/local/bin/mini-tracker" 2>/dev/null || true
rm -f "/usr/local/bin/mini-tracker-server" 2>/dev/null || true

# 4. Remove desktop shortcuts & autostart launchers
rm -f "${HOME}/.local/share/applications/get-hike"*.desktop
rm -f "${HOME}/.local/share/applications/mini-tracker"*.desktop
rm -f "${HOME}/.local/share/applications/localhost.desktop"
rm -f "${HOME}/.config/autostart/get-hike.desktop"
rm -f "${HOME}/.config/autostart/mini-tracker.desktop"

# 5. Remove icons & pixmaps
rm -f "${HOME}/.local/share/icons/hicolor/512x512/apps/get-hike"*.png
rm -f "${HOME}/.local/share/icons/hicolor/512x512/apps/mini-tracker"*.png
rm -f "${HOME}/.local/share/icons/hicolor/512x512/apps/localhost.png"
rm -f "${HOME}/.local/share/pixmaps/get-hike"*.png
rm -f "${HOME}/.local/share/pixmaps/mini-tracker"*.png
rm -f "${HOME}/.local/share/pixmaps/localhost.png"

# 6. Remove data and config directories
rm -rf "${HOME}/.local/share/get-hike"
rm -rf "${HOME}/.local/share/mini-tracker"
rm -rf "${HOME}/.local/share/mini-tracker-server"
rm -rf "${HOME}/.local/share/mini-tracker-tmp"*
rm -rf "${HOME}/.config/get-hike"
rm -rf "${HOME}/.config/mini-tracker"

# 7. Update caches
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "${HOME}/.local/share/applications" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "${HOME}/.local/share/icons/hicolor" 2>/dev/null || true
fi

echo "✅ ${APP_NAME} & legacy builds have been completely cleaned from your local system."
