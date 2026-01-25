#!/bin/bash

set -e

REPO="altlimit/sitegen"
INSTALL_DIR="$HOME/.altlimit/bin"
BIN_NAME="sitegen"

echo "Installing $BIN_NAME from $REPO..."

add_to_path() {
  shell_profile=""
  case "$SHELL" in
    */zsh) shell_profile="$HOME/.zshrc" ;;
    */bash) shell_profile="$HOME/.bashrc" ;;
    *) shell_profile="$HOME/.profile" ;;
  esac

  if [ -f "$shell_profile" ]; then
    if ! grep -q "$INSTALL_DIR" "$shell_profile"; then
      echo "Adding $INSTALL_DIR to PATH in $shell_profile"
      echo "export PATH=\$PATH:$INSTALL_DIR" >> "$shell_profile"
      echo "Restart your terminal or run 'source $shell_profile' to use sitegen directly."
    else
      echo "$INSTALL_DIR is already in $shell_profile"
    fi
  else
    echo "Could not detect shell profile. Please add $INSTALL_DIR to your PATH manually."
  fi
}

install_binary() {
  URL="$1"
  mkdir -p "$INSTALL_DIR"
  
  echo "Downloading $URL..."
  TEMP_FILE=$(mktemp)
  if curl -s -S -L -o "$TEMP_FILE" "$URL"; then
    if [[ "$URL" == *.zip ]]; then
        unzip -q -o "$TEMP_FILE" -d "$INSTALL_DIR"
    else
        tar -xzf "$TEMP_FILE" -C "$INSTALL_DIR"
    fi
    rm "$TEMP_FILE"
  else
    echo "Download failed."
    rm "$TEMP_FILE"
    exit 1
  fi

  chmod +x "$INSTALL_DIR/$BIN_NAME"
  add_to_path
  echo "$BIN_NAME installed successfully at $INSTALL_DIR/$BIN_NAME"
}

OS="$(uname -s)"
ARCH="$(uname -m)"
DOWNLOAD_URL=""

case "$OS" in
    Linux)
        if [ "$ARCH" == "x86_64" ]; then
            DOWNLOAD_URL="https://github.com/$REPO/releases/download/latest/linux.tgz"
        else
            echo "Unsupported architecture: $ARCH for Linux"
            exit 1
        fi
        ;;
    Darwin)
        if [ "$ARCH" == "x86_64" ]; then
            DOWNLOAD_URL="https://github.com/$REPO/releases/download/latest/darwin.tgz"
        elif [ "$ARCH" == "arm64" ]; then
            DOWNLOAD_URL="https://github.com/$REPO/releases/download/latest/darwin-arm64.tgz"
        else
            echo "Unsupported architecture: $ARCH for macOS"
            exit 1
        fi
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

install_binary "$DOWNLOAD_URL"
