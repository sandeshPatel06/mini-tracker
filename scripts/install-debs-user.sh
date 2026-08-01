#!/usr/bin/env bash
set -e

INSTALL_DIR="${HOME}/.local"
DOWNLOAD_DIR="/tmp/wails-debs-download"
ENV_FILE="${INSTALL_DIR}/wails_env.sh"
ZSHRC="${HOME}/.zshrc"

echo "=================================================="
echo "Installing GTK3 & WebKit2GTK dependencies (non-sudo)..."
echo "Target directory: ${INSTALL_DIR}"
echo "=================================================="

mkdir -p "${INSTALL_DIR}/bin" "${INSTALL_DIR}/usr/bin" "${INSTALL_DIR}/usr/lib/x86_64-linux-gnu"
rm -rf "${DOWNLOAD_DIR}"
mkdir -p "${DOWNLOAD_DIR}"

cd "${DOWNLOAD_DIR}"

DEB_PACKAGES=(
  pkg-config
  pkgconf
  pkgconf-bin
  libpkgconf3
  libgtk-3-dev
  libgtk-3-0t64
  libwebkit2gtk-4.1-dev
  libwebkit2gtk-4.1-0
  libjavascriptcoregtk-4.1-dev
  libjavascriptcoregtk-4.1-0
  libsoup-3.0-dev
  libsoup-3.0-0
  libglib2.0-dev
  libglib2.0-0t64
  libcairo2-dev
  libcairo2
  libcairo-gobject2
  libpango1.0-dev
  libpango-1.0-0
  libpangocairo-1.0-0
  libatk1.0-dev
  libatk1.0-0t64
  libatk-bridge2.0-dev
  libatk-bridge2.0-0t64
  libatspi2.0-dev
  libgdk-pixbuf-2.0-dev
  libgdk-pixbuf-2.0-0
  libepoxy-dev
  libwayland-dev
  libx11-dev
  libxcomposite-dev
  libxcursor-dev
  libxdamage-dev
  libxext-dev
  libxfixes-dev
  libxi-dev
  libxinerama-dev
  libxrandr-dev
  libxrender-dev
  libxtst-dev
  libdbus-1-dev
  libsecret-1-dev
  libharfbuzz-dev
  libharfbuzz0b
  libfontconfig-dev
  libfreetype-dev
  libffi-dev
  zlib1g-dev
  libpcre2-dev
  libpng-dev
  libgraphite2-dev
  x11proto-dev
  x11proto-core-dev
  x11proto-xext-dev
  x11proto-render-dev
  libmount-dev
  libselinux1-dev
  libtiff-dev
  libsepol-dev
  libblkid-dev
  liblzma-dev
  libdeflate-dev
  libsysprof-capture-4-dev
  libsqlite3-dev
  libnghttp2-dev
  libjpeg-dev
  libicu-dev
  libxcb1-dev
  libxcb-render0-dev
  libxcb-shm0-dev
  libpixman-1-dev
  libbrotli-dev
  libwebp-dev
  libpsl-dev
  libxau-dev
  libxdmcp-dev
  libsharpyuv-dev
  libfribidi-dev
  libthai-dev
  libxft-dev
  libdatrie-dev
  libjpeg-turbo8-dev
  libxkbcommon-dev
  libegl-dev
  libgl-dev
  libgles-dev
  libglvnd-dev
)

echo "--> Downloading .deb packages without sudo..."
apt-get download "${DEB_PACKAGES[@]}" 2>&1

echo "--> Extracting .deb packages into ${INSTALL_DIR}..."
for deb in *.deb; do
  if [ -f "$deb" ]; then
    dpkg-deb -x "$deb" "${INSTALL_DIR}" 2>/dev/null || true
  fi
done

rm -rf "${DOWNLOAD_DIR}"

# Ensure pkg-config symlinks are properly established
if [ -f "${INSTALL_DIR}/usr/bin/pkgconf" ]; then
  ln -sf pkgconf "${INSTALL_DIR}/usr/bin/pkg-config"
  ln -sf "${INSTALL_DIR}/usr/bin/pkgconf" "${INSTALL_DIR}/bin/pkgconf"
  ln -sf "${INSTALL_DIR}/usr/bin/pkg-config" "${INSTALL_DIR}/bin/pkg-config"
fi

# Fix prefix inside all unpacked .pc files
echo "--> Adjusting pkg-config metadata paths in ${INSTALL_DIR}..."
find "${INSTALL_DIR}" -name "*.pc" -exec sed -i "s|^prefix=/usr$|prefix=${INSTALL_DIR}/usr|g" {} +

# Create missing .so development symlinks for runtime libraries
echo "--> Ensuring shared library symlinks exist in ${INSTALL_DIR}/usr/lib/x86_64-linux-gnu..."
if [ -d "${INSTALL_DIR}/usr/lib/x86_64-linux-gnu" ]; then
  cd "${INSTALL_DIR}/usr/lib/x86_64-linux-gnu"
  for lib in *.so.*; do
    if [ -f "$lib" ] || [ -L "$lib" ]; then
      base=$(echo "$lib" | sed -E 's/\.so\..*$/.so/')
      if [ ! -e "$base" ]; then
        ln -sf "$lib" "$base"
      fi
    fi
  done
fi

echo "--> Creating environment configuration in ${ENV_FILE}..."
cat << 'EOF' > "${ENV_FILE}"
# Wails GTK/WebKit2GTK local environment paths
export PATH="$HOME/.local/bin:$HOME/.local/usr/bin:$PATH"
export PKG_CONFIG_PATH="$HOME/.local/usr/lib/x86_64-linux-gnu/pkgconfig:$HOME/.local/usr/lib/pkgconfig:$HOME/.local/usr/share/pkgconfig:/usr/lib/x86_64-linux-gnu/pkgconfig:/usr/share/pkgconfig:/usr/lib/pkgconfig:${PKG_CONFIG_PATH}"
export LIBRARY_PATH="$HOME/.local/usr/lib/x86_64-linux-gnu:$HOME/.local/usr/lib:${LIBRARY_PATH}"
export LD_LIBRARY_PATH="$HOME/.local/usr/lib/x86_64-linux-gnu:$HOME/.local/usr/lib:${LD_LIBRARY_PATH}"
export C_INCLUDE_PATH="$HOME/.local/usr/include:$HOME/.local/usr/include/x86_64-linux-gnu:${C_INCLUDE_PATH}"
export CPLUS_INCLUDE_PATH="$HOME/.local/usr/include:$HOME/.local/usr/include/x86_64-linux-gnu:${CPLUS_INCLUDE_PATH}"
export CGO_CFLAGS="-I$HOME/.local/usr/include -I$HOME/.local/usr/include/x86_64-linux-gnu ${CGO_CFLAGS}"
export CGO_LDFLAGS="-L$HOME/.local/usr/lib -L$HOME/.local/usr/lib/x86_64-linux-gnu ${CGO_LDFLAGS}"
EOF

chmod +x "${ENV_FILE}"

echo "--> Setting up sourcing in ${ZSHRC}..."
SOURCE_LINE="[ -f \"${ENV_FILE}\" ] && source \"${ENV_FILE}\""

if [ -f "${ZSHRC}" ]; then
  if ! grep -Fq "wails_env.sh" "${ZSHRC}"; then
    echo "" >> "${ZSHRC}"
    echo "# Wails Local GTK/WebKit2GTK Environment" >> "${ZSHRC}"
    echo "${SOURCE_LINE}" >> "${ZSHRC}"
    echo "✓ Appended source line to ${ZSHRC}"
  else
    echo "✓ Sourcing already present in ${ZSHRC}"
  fi
else
  echo "# Wails Local GTK/WebKit2GTK Environment" > "${ZSHRC}"
  echo "${SOURCE_LINE}" >> "${ZSHRC}"
  echo "✓ Created ${ZSHRC} and added source line"
fi

echo "=================================================="
echo "✓ Installation complete!"
echo "To apply in your current terminal run:"
echo "  source ${ENV_FILE}"
echo "=================================================="
