#!/usr/bin/env bash
set -euo pipefail

# memoryd installer — one command to install and start everything.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/memory-daemon/memoryd/main/install.sh | bash
#   curl -fsSL ... | bash -s -- --atlas "mongodb+srv://..."

REPO="memory-daemon/memoryd"
ATLAS_URI=""
INSTALL_DIR="/usr/local/bin"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --atlas)   ATLAS_URI="$2"; shift 2 ;;
    --dir)     INSTALL_DIR="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; echo "Usage: $0 [--atlas <uri>] [--dir <install_path>]"; exit 1 ;;
  esac
done

MEMORYD_DIR="$HOME/.memoryd"
MODEL_DIR="$MEMORYD_DIR/models"
MODEL_PATH="$MODEL_DIR/voyage-4-nano.gguf"
MODEL_URL="https://huggingface.co/jsonMartin/voyage-4-nano-gguf/resolve/main/voyage-4-nano-q8_0.gguf?download=true"
CONFIG_PATH="$MEMORYD_DIR/config.yaml"

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; }
info() { printf '  \033[34m→\033[0m %s\n' "$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }

# ── Detect platform ────────────────────────────────────────────────

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *) fail "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# ── Pre-flight checks ─────────────────────────────────────────────

step "Pre-flight checks"

# curl or wget
if command -v curl &>/dev/null; then
  DOWNLOAD="curl -fSL"
  DOWNLOAD_QUIET="curl -fsSL"
  DOWNLOAD_PROGRESS="curl -fL --progress-bar"
elif command -v wget &>/dev/null; then
  DOWNLOAD="wget -qO-"
  DOWNLOAD_QUIET="wget -qO-"
  DOWNLOAD_PROGRESS="wget -q --show-progress -O"
else
  fail "curl or wget required"; exit 1
fi
ok "Download tool available"

# llama.cpp
if command -v llama-server &>/dev/null; then
  ok "llama-server installed"
else
  info "Installing llama.cpp from GitHub release..."
  LLAMA_REPO="ggerganov/llama.cpp"
  LLAMA_TAG=$($DOWNLOAD_QUIET "https://api.github.com/repos/$LLAMA_REPO/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
  if [[ -z "$LLAMA_TAG" ]]; then
    fail "Could not determine latest llama.cpp release"
    exit 1
  fi
  info "Latest llama.cpp release: $LLAMA_TAG"

  if [[ "$OS" == "darwin" ]]; then
    LLAMA_ASSET="llama-${LLAMA_TAG}-bin-macos-arm64.tar.gz"
    if [[ "$ARCH" == "amd64" ]]; then
      LLAMA_ASSET="llama-${LLAMA_TAG}-bin-macos-x64.tar.gz"
    fi
  elif [[ "$OS" == "linux" ]]; then
    LLAMA_ASSET="llama-${LLAMA_TAG}-bin-ubuntu-x64.tar.gz"
  else
    fail "No prebuilt llama.cpp for $OS/$ARCH"
    exit 1
  fi

  LLAMA_URL="https://github.com/$LLAMA_REPO/releases/download/${LLAMA_TAG}/${LLAMA_ASSET}"
  LLAMA_TMPDIR=$(mktemp -d)
  if ! $DOWNLOAD_PROGRESS -o "$LLAMA_TMPDIR/$LLAMA_ASSET" "$LLAMA_URL" 2>&1; then
    fail "Could not download llama.cpp from $LLAMA_URL"
    rm -rf "$LLAMA_TMPDIR"
    exit 1
  fi
  mkdir -p "$LLAMA_TMPDIR/llama"
  tar -xzf "$LLAMA_TMPDIR/$LLAMA_ASSET" -C "$LLAMA_TMPDIR/llama"

  # Find llama-server in the extracted archive.
  LLAMA_SERVER=$(find "$LLAMA_TMPDIR/llama" -name "llama-server" -type f | head -1)
  if [[ -z "$LLAMA_SERVER" ]]; then
    fail "llama-server not found in release archive"
    rm -rf "$LLAMA_TMPDIR"
    exit 1
  fi

  chmod +x "$LLAMA_SERVER"
  if [[ -w "$INSTALL_DIR" ]]; then
    cp "$LLAMA_SERVER" "$INSTALL_DIR/llama-server"
  else
    sudo cp "$LLAMA_SERVER" "$INSTALL_DIR/llama-server"
  fi
  rm -rf "$LLAMA_TMPDIR"
  # Strip macOS quarantine attribute so unsigned binary isn't killed.
  [[ "$OS" == "darwin" ]] && xattr -d com.apple.quarantine "$INSTALL_DIR/llama-server" 2>/dev/null || true
  ok "llama-server → $INSTALL_DIR/llama-server"
fi

if [[ -n "$ATLAS_URI" ]]; then
  ok "Using Atlas connection string"
fi

# ── Download release ───────────────────────────────────────────────

step "Download memoryd"

LATEST_TAG=$($DOWNLOAD_QUIET "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || echo "")

if [[ -z "$LATEST_TAG" ]]; then
  fail "Could not determine latest release from GitHub"
  info "Check https://github.com/$REPO/releases"
  info "Or build from source: git clone https://github.com/$REPO && cd memoryd && make build"
  exit 1
fi

info "Latest release: $LATEST_TAG"

VERSION="${LATEST_TAG#v}"  # strip leading v
ARCHIVE_NAME="memoryd_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/${LATEST_TAG}/${ARCHIVE_NAME}"

TMPDIR_DL=$(mktemp -d)
trap 'rm -rf "$TMPDIR_DL"' EXIT

info "Downloading $ARCHIVE_NAME..."
if ! $DOWNLOAD_PROGRESS -o "$TMPDIR_DL/$ARCHIVE_NAME" "$DOWNLOAD_URL" 2>&1; then
  fail "Download failed — archive may not exist for $OS/$ARCH"
  info "Check https://github.com/$REPO/releases/tag/$LATEST_TAG"
  info "Or build from source: git clone https://github.com/$REPO && cd memoryd && make build"
  exit 1
fi

tar -xzf "$TMPDIR_DL/$ARCHIVE_NAME" -C "$TMPDIR_DL"
chmod +x "$TMPDIR_DL/memoryd"
ok "Archive extracted"

# Install to target directory.
if [[ -w "$INSTALL_DIR" ]]; then
  mv "$TMPDIR_DL/memoryd" "$INSTALL_DIR/memoryd"
else
  info "Installing to $INSTALL_DIR (requires sudo)..."
  sudo mv "$TMPDIR_DL/memoryd" "$INSTALL_DIR/memoryd"
fi

MEMORYD_BIN="$INSTALL_DIR/memoryd"
# Strip macOS quarantine attribute so unsigned binary isn't killed.
[[ "$OS" == "darwin" ]] && xattr -d com.apple.quarantine "$MEMORYD_BIN" 2>/dev/null || true
ok "memoryd binary → $MEMORYD_BIN"

# Install macOS menu bar app (separate release asset).
APP_DIR="/Applications"
if [[ "$OS" == "darwin" ]]; then
  APP_ZIP_NAME="Memoryd-darwin-${ARCH}.zip"
  APP_ZIP_URL="https://github.com/$REPO/releases/download/${LATEST_TAG}/${APP_ZIP_NAME}"
  APP_TMPDIR=$(mktemp -d)
  info "Downloading Memoryd.app..."
  if $DOWNLOAD_QUIET -o "$APP_TMPDIR/$APP_ZIP_NAME" "$APP_ZIP_URL" 2>/dev/null; then
    pkill -f "Memoryd.app" 2>/dev/null || true
    sleep 0.5
    rm -rf "$APP_DIR/Memoryd.app"
    unzip -q "$APP_TMPDIR/$APP_ZIP_NAME" -d "$APP_DIR"
    # Strip macOS quarantine attribute from the entire .app bundle.
    xattr -dr com.apple.quarantine "$APP_DIR/Memoryd.app" 2>/dev/null || true
    ok "Memoryd.app → $APP_DIR/Memoryd.app"
  else
    info "Memoryd.app not available for $ARCH — menu bar app skipped"
  fi
  rm -rf "$APP_TMPDIR"
fi

trap - EXIT
rm -rf "$TMPDIR_DL"

# ── MongoDB ────────────────────────────────────────────────────────

step "MongoDB"

if [[ -n "$ATLAS_URI" ]]; then
  MONGO_URI="$ATLAS_URI"
  ok "Using Atlas: ${ATLAS_URI:0:30}..."
  info "Ensure you have a vector search index named 'vector_index'"
  info "on the 'memories' collection (numDimensions: 1024, similarity: cosine)"
else
  MONGO_URI=""
  ok "Skipped — connect from the Memoryd menu bar app after install"
fi

# ── Embedding model ────────────────────────────────────────────────

step "Embedding model"

mkdir -p "$MODEL_DIR"

if [[ -f "$MODEL_PATH" ]]; then
  ok "Model already downloaded"
else
  info "Downloading voyage-4-nano (Q8_0, ~70MB)..."
  $DOWNLOAD_PROGRESS -o "$MODEL_PATH" "$MODEL_URL"
  ok "Model downloaded"
fi

# ── Config ─────────────────────────────────────────────────────────

step "Config"

mkdir -p "$MEMORYD_DIR"

if [[ -n "$MONGO_URI" ]]; then
  # Store MongoDB URI securely in OS keychain (macOS Keychain / Linux secret-tool).
  KEYCHAIN_OK=false
  if [[ "$OS" == "darwin" ]]; then
    /usr/bin/security add-generic-password -s memoryd -a mongodb_atlas_uri -w "$MONGO_URI" -U 2>/dev/null \
      && KEYCHAIN_OK=true
  elif command -v secret-tool &>/dev/null; then
    echo -n "$MONGO_URI" | secret-tool store --label "memoryd mongodb_atlas_uri" service memoryd key mongodb_atlas_uri 2>/dev/null \
      && KEYCHAIN_OK=true
  fi

  if $KEYCHAIN_OK; then
    CONFIG_URI="keychain:mongodb_atlas_uri"
    ok "MongoDB URI stored in OS keychain"
  else
    CONFIG_URI="$MONGO_URI"
    info "OS keychain not available — URI stored in config file (less secure)"
  fi
else
  CONFIG_URI=""
fi

if [[ -f "$CONFIG_PATH" ]]; then
  ok "Config exists at $CONFIG_PATH"
  if [[ -n "$CONFIG_URI" ]] && grep -q 'mongodb_atlas_uri:' "$CONFIG_PATH"; then
    info "Updating mongodb_atlas_uri in existing config"
    if [[ "$OS" == "darwin" ]]; then
      sed -i '' "s|mongodb_atlas_uri:.*|mongodb_atlas_uri: \"$CONFIG_URI\"|" "$CONFIG_PATH"
    else
      sed -i "s|mongodb_atlas_uri:.*|mongodb_atlas_uri: \"$CONFIG_URI\"|" "$CONFIG_PATH"
    fi
  fi
else
  cat > "$CONFIG_PATH" << EOF
port: 7432
mongodb_atlas_uri: "$CONFIG_URI"
mongodb_database: memoryd
model_path: ~/.memoryd/models/voyage-4-nano.gguf
embedding_dim: 1024
retrieval_top_k: 5
retrieval_max_tokens: 2048
upstream_anthropic_url: https://api.anthropic.com
EOF
  ok "Config written to $CONFIG_PATH"
fi

# ── Claude Code MCP ────────────────────────────────────────────────

step "Claude Code MCP"

MCP_CONFIG="$HOME/.mcp.json"

if [[ -f "$MCP_CONFIG" ]]; then
  if grep -q '"memoryd"' "$MCP_CONFIG" 2>/dev/null; then
    ok "memoryd already in $MCP_CONFIG"
  else
    info "Adding memoryd to existing $MCP_CONFIG"
    python3 -c "
import json
with open('$MCP_CONFIG') as f:
    cfg = json.load(f)
cfg.setdefault('mcpServers', {})['memoryd'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$MCP_CONFIG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added memoryd to $MCP_CONFIG" || fail "Could not update $MCP_CONFIG — add manually"
  fi
else
  cat > "$MCP_CONFIG" << EOF
{
  "mcpServers": {
    "memoryd": {
      "command": "$MEMORYD_BIN",
      "args": ["mcp"]
    }
  }
}
EOF
  ok "Created $MCP_CONFIG with memoryd MCP server"
fi

# ── Claude Desktop MCP ─────────────────────────────────────────────

CLAUDE_CONFIG_DIR="$HOME/Library/Application Support/Claude"
CLAUDE_CONFIG="$CLAUDE_CONFIG_DIR/claude_desktop_config.json"

if [[ "$OS" == "darwin" && -d "$CLAUDE_CONFIG_DIR" ]]; then
  if [[ -f "$CLAUDE_CONFIG" ]]; then
    if grep -q '"memoryd"' "$CLAUDE_CONFIG" 2>/dev/null; then
      ok "memoryd already in Claude Desktop config"
    else
      info "Adding memoryd to Claude Desktop config"
      python3 -c "
import json
with open('$CLAUDE_CONFIG') as f:
    cfg = json.load(f)
cfg.setdefault('mcpServers', {})['memoryd'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$CLAUDE_CONFIG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added memoryd to Claude Desktop config" || info "Could not update Claude Desktop config — add manually"
    fi
  fi
fi

# ── Cursor MCP ─────────────────────────────────────────────────────

CURSOR_MCP="$HOME/.cursor/mcp.json"

if [[ -d "$HOME/.cursor" ]]; then
  step "Cursor MCP"
  if [[ -f "$CURSOR_MCP" ]]; then
    if grep -q '"memoryd"' "$CURSOR_MCP" 2>/dev/null; then
      ok "memoryd already in Cursor config"
    else
      info "Adding memoryd to Cursor config"
      python3 -c "
import json
with open('$CURSOR_MCP') as f:
    cfg = json.load(f)
cfg.setdefault('mcpServers', {})['memoryd'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$CURSOR_MCP', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added memoryd to Cursor config" || info "Could not update Cursor config — add manually"
    fi
  else
    cat > "$CURSOR_MCP" << EOF
{
  "mcpServers": {
    "memoryd": {
      "command": "$MEMORYD_BIN",
      "args": ["mcp"]
    }
  }
}
EOF
    ok "Created $CURSOR_MCP with memoryd MCP server"
  fi
fi

# ── Windsurf MCP ───────────────────────────────────────────────────

WINDSURF_DIR="$HOME/.codeium/windsurf"
WINDSURF_MCP="$WINDSURF_DIR/mcp_config.json"

if [[ -d "$WINDSURF_DIR" ]]; then
  step "Windsurf MCP"
  if [[ -f "$WINDSURF_MCP" ]]; then
    if grep -q '"memoryd"' "$WINDSURF_MCP" 2>/dev/null; then
      ok "memoryd already in Windsurf config"
    else
      info "Adding memoryd to Windsurf config"
      python3 -c "
import json
with open('$WINDSURF_MCP') as f:
    cfg = json.load(f)
cfg.setdefault('mcpServers', {})['memoryd'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$WINDSURF_MCP', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added memoryd to Windsurf config" || info "Could not update Windsurf config — add manually"
    fi
  else
    cat > "$WINDSURF_MCP" << EOF
{
  "mcpServers": {
    "memoryd": {
      "command": "$MEMORYD_BIN",
      "args": ["mcp"]
    }
  }
}
EOF
    ok "Created $WINDSURF_MCP with memoryd MCP server"
  fi
fi

# ── Start everything ───────────────────────────────────────────────

step "Starting memoryd"

# Stop any existing daemon.
if curl -sf "http://127.0.0.1:7432/health" >/dev/null 2>&1; then
  info "Stopping existing daemon..."
  curl -sf -X POST "http://127.0.0.1:7432/shutdown" >/dev/null 2>&1 || true
  sleep 1
fi

if [[ "$OS" == "darwin" && -d "$APP_DIR/Memoryd.app" ]]; then
  # Launch the menu bar app — it manages the daemon.
  open "$APP_DIR/Memoryd.app"
  ok "Memoryd.app launched (menu bar + daemon)"
else
  # Linux or no .app: start daemon in background.
  nohup "$MEMORYD_BIN" start > "$MEMORYD_DIR/daemon.log" 2>&1 &
  ok "Daemon started in background (PID $!)"
fi

# Wait for daemon to be healthy.
info "Waiting for daemon to be ready..."
for i in $(seq 1 20); do
  if curl -sf "http://127.0.0.1:7432/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if curl -sf "http://127.0.0.1:7432/health" >/dev/null 2>&1; then
  ok "Daemon is healthy"
elif [[ -n "$ATLAS_URI" ]]; then
  fail "Daemon did not start — check $MEMORYD_DIR/daemon.log"
else
  info "Daemon will start after MongoDB is connected from the menu bar app"
fi

# ── Done ───────────────────────────────────────────────────────────

step "Ready!"

echo ""
if [[ -n "$ATLAS_URI" ]]; then
  echo "  memoryd is running and ready to use."
  echo ""
  echo "  Dashboard:  http://127.0.0.1:7432"
  echo "  MCP mode:   Already configured in ~/.mcp.json"
else
  echo "  memoryd is installed. One more step:"
  echo ""
  if [[ "$OS" == "darwin" && -d "$APP_DIR/Memoryd.app" ]]; then
  echo "  Click the M icon in your menu bar and select 'Connect to MongoDB'"
  echo "  to connect to a local (Docker) or remote (Atlas) database."
  else
  echo "  Run: memoryd config --mongo <uri>   (or set mongodb_atlas_uri in ~/.memoryd/config.yaml)"
  fi
fi
echo ""
if [[ "$OS" == "darwin" && -d "$APP_DIR/Memoryd.app" ]]; then
echo "  The Memoryd menu bar app is running — look for M in your menu bar."
echo ""
fi
