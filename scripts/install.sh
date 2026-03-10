#!/usr/bin/env bash
#
# Zion Node installer
# Usage: curl -fsSL https://raw.githubusercontent.com/THEZIONLABS/Zion-Node/main/scripts/install.sh | bash
#
set -euo pipefail

REPO="THEZIONLABS/Zion-Node"
BINARY="zion-node"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
detect_platform() {
  local os arch

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"

  case "$os" in
    linux)  os="linux" ;;
    darwin) os="darwin" ;;
    *)
      echo "Error: Unsupported OS: $os" >&2
      exit 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)
      echo "Error: Unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac

  echo "${os}-${arch}"
}

# Get the latest release tag from GitHub API
get_latest_version() {
  # Try /releases/latest first (stable releases only), fall back to
  # /releases (includes pre-releases) if no stable release exists yet.
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  local fallback_url="https://api.github.com/repos/${REPO}/releases"
  local tag

  if command -v curl &>/dev/null; then
    tag="$(curl -fsSL "$url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    if [ -z "$tag" ]; then
      tag="$(curl -fsSL "$fallback_url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    fi
  elif command -v wget &>/dev/null; then
    tag="$(wget -qO- "$url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    if [ -z "$tag" ]; then
      tag="$(wget -qO- "$fallback_url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    fi
  else
    echo "Error: curl or wget is required" >&2
    exit 1
  fi

  if [ -z "$tag" ]; then
    echo "Error: Could not determine latest version" >&2
    exit 1
  fi

  echo "$tag"
}

main() {
  local platform version version_num archive url tmpdir

  echo "==> Detecting platform..."
  platform="$(detect_platform)"
  echo "    Platform: ${platform}"

  echo "==> Fetching latest version..."
  version="$(get_latest_version)"
  version_num="${version#v}"
  echo "    Version: ${version}"

  archive="${BINARY}-${version_num}-${platform}.tar.gz"
  url="https://github.com/${REPO}/releases/download/${version}/${archive}"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  echo "==> Downloading ${archive}..."
  if command -v curl &>/dev/null; then
    curl -fsSL -o "${tmpdir}/${archive}" "$url"
  else
    wget -qO "${tmpdir}/${archive}" "$url"
  fi

  echo "==> Extracting..."
  tar -xzf "${tmpdir}/${archive}" -C "$tmpdir"

  echo "==> Installing to ${INSTALL_DIR}/${BINARY}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "${tmpdir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    sudo mv "${tmpdir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi
  chmod +x "${INSTALL_DIR}/${BINARY}"

  echo ""
  echo "==> zion-node installed successfully!"
  echo "    Version: $(${INSTALL_DIR}/${BINARY} version 2>/dev/null || echo "$version")"
  echo ""
  echo "    Next steps:"
  echo "      zion-node wallet new"
  echo "      cp config.example.toml ~/.zion-node/config.toml"
  echo "      zion-node --config ~/.zion-node/config.toml"
}

main "$@"
