#!/usr/bin/env bash
set -euo pipefail

# pgmemory dev installer — build from source and run locally.
# Usage:
#   ./install-dev.sh                                   # embedded PostgreSQL (default)
#   ./install-dev.sh --postgres "postgres://host/db"   # external PostgreSQL

POSTGRES_URI=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --postgres) POSTGRES_URI="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; echo "Usage: $0 [--postgres <connection_string>]"; exit 1 ;;
  esac
done

MEMORYD_DIR="$HOME/.pgmemory"
MODEL_DIR="$MEMORYD_DIR/models"
MODEL_PATH="$MODEL_DIR/voyage-4-nano.gguf"
MODEL_URL="https://huggingface.co/jsonMartin/voyage-4-nano-gguf/resolve/main/voyage-4-nano-q8_0.gguf?download=true"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_PATH="$MEMORYD_DIR/config.yaml"

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; }
info() { printf '  \033[34m→\033[0m %s\n' "$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }

# ── Pre-flight checks ──────────────────────────────────────────────

step "Pre-flight checks"

# Go
if command -v go &>/dev/null; then
  GO_VER=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | head -1)
  ok "Go installed ($GO_VER)"
else
  fail "Go is not installed"
  info "Install with: brew install go"
  exit 1
fi

# llama.cpp
if command -v llama-server &>/dev/null; then
  ok "llama-server installed"
else
  info "Installing llama.cpp from GitHub release..."
  OS_NAME="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH_NAME="$(uname -m)"
  LLAMA_REPO="ggerganov/llama.cpp"
  LLAMA_TAG=$(curl -fsSL "https://api.github.com/repos/$LLAMA_REPO/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
  if [[ -z "$LLAMA_TAG" ]]; then
    fail "Could not determine latest llama.cpp release"
    exit 1
  fi
  if [[ "$OS_NAME" == "darwin" ]]; then
    LLAMA_ASSET="llama-${LLAMA_TAG}-bin-macos-arm64.tar.gz"
    [[ "$ARCH_NAME" == "x86_64" ]] && LLAMA_ASSET="llama-${LLAMA_TAG}-bin-macos-x64.tar.gz"
  else
    LLAMA_ASSET="llama-${LLAMA_TAG}-bin-ubuntu-x64.tar.gz"
  fi
  LLAMA_TMPDIR=$(mktemp -d)
  curl -fL --progress-bar -o "$LLAMA_TMPDIR/$LLAMA_ASSET" "https://github.com/$LLAMA_REPO/releases/download/${LLAMA_TAG}/${LLAMA_ASSET}"
  mkdir -p "$LLAMA_TMPDIR/llama"
  tar -xzf "$LLAMA_TMPDIR/$LLAMA_ASSET" -C "$LLAMA_TMPDIR/llama"
  LLAMA_SERVER=$(find "$LLAMA_TMPDIR/llama" -name "llama-server" -type f | head -1)
  if [[ -z "$LLAMA_SERVER" ]]; then
    fail "llama-server not found in release archive"
    rm -rf "$LLAMA_TMPDIR"
    exit 1
  fi
  chmod +x "$LLAMA_SERVER"
  sudo cp "$LLAMA_SERVER" /usr/local/bin/llama-server
  rm -rf "$LLAMA_TMPDIR"
  ok "llama-server → /usr/local/bin/llama-server"
fi

# pgvector (for embedded PostgreSQL)
if [[ "$(uname)" == "Darwin" ]]; then
  if brew list pgvector &>/dev/null 2>&1; then
    ok "pgvector installed (Homebrew)"
  else
    info "Installing pgvector via Homebrew..."
    brew install pgvector
    ok "pgvector installed"
  fi
else
  if dpkg -l | grep -q postgresql.*pgvector 2>/dev/null || [[ -f /usr/share/postgresql/*/extension/vector.control ]]; then
    ok "pgvector available"
  else
    info "pgvector not found — embedded PostgreSQL will attempt to use it"
    info "Install with: sudo apt install postgresql-16-pgvector (or equivalent)"
  fi
fi

if [[ -n "$POSTGRES_URI" ]]; then
  ok "Using external PostgreSQL"
fi

# ── Database ─────────────────────────────────────────────────────────

step "Database"

if [[ -n "$POSTGRES_URI" ]]; then
  ok "Using external PostgreSQL: ${POSTGRES_URI:0:30}..."
  info "Ensure pgvector extension is available on the target database"
else
  ok "Using embedded PostgreSQL (no external database needed)"
  info "Data will be stored in ~/.pgmemory/data/ on port 7434"
fi

# ── Embedding model ─────────────────────────────────────────────────

step "Embedding model"

mkdir -p "$MODEL_DIR"

if [[ -f "$MODEL_PATH" ]]; then
  ok "Model already downloaded"
else
  info "Downloading voyage-4-nano (Q8_0, ~354MB)..."
  curl -L --progress-bar -o "$MODEL_PATH" "$MODEL_URL"
  ok "Model downloaded"
fi

# ── Config ──────────────────────────────────────────────────────────

step "Config"

mkdir -p "$MEMORYD_DIR"

if [[ -f "$CONFIG_PATH" ]]; then
  ok "Config exists at $CONFIG_PATH"
else
  if [[ -n "$POSTGRES_URI" ]]; then
    cat > "$CONFIG_PATH" << EOF
port: 7432
mode: proxy
postgres_url: "$POSTGRES_URI"
model_path: ~/.pgmemory/models/voyage-4-nano.gguf
embedding_dim: 1024
retrieval_top_k: 5
retrieval_max_tokens: 2048
upstream_anthropic_url: https://api.anthropic.com
llm_synthesis: false
EOF
  else
    cat > "$CONFIG_PATH" << EOF
port: 7432
mode: proxy
model_path: ~/.pgmemory/models/voyage-4-nano.gguf
embedding_dim: 1024
retrieval_top_k: 5
retrieval_max_tokens: 2048
upstream_anthropic_url: https://api.anthropic.com
llm_synthesis: false
EOF
  fi
  ok "Config written to $CONFIG_PATH"
fi

# ── Build ───────────────────────────────────────────────────────────

step "Build"

cd "$SCRIPT_DIR"
info "Building pgmemory + tray app..."
make app 2>&1
MEMORYD_BIN="$SCRIPT_DIR/bin/pgmemory"
ok "bin/pgmemory and Pgmemory.app built"

# ── Claude Code MCP config ─────────────────────────────────────────

step "Claude Code MCP"

MCP_CONFIG="$HOME/.mcp.json"

if [[ -f "$MCP_CONFIG" ]]; then
  if grep -q '"pgmemory"' "$MCP_CONFIG" 2>/dev/null; then
    ok "pgmemory already in $MCP_CONFIG"
  else
    info "Adding pgmemory to existing $MCP_CONFIG"
    python3 -c "
import json
with open('$MCP_CONFIG') as f:
    cfg = json.load(f)
servers = cfg.setdefault('mcpServers', {})
servers.pop('memoryd', None)  # remove legacy entry
servers['pgmemory'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$MCP_CONFIG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added pgmemory to $MCP_CONFIG" || fail "Could not update $MCP_CONFIG — add manually"
  fi
else
  cat > "$MCP_CONFIG" << EOF
{
  "mcpServers": {
    "pgmemory": {
      "command": "$MEMORYD_BIN",
      "args": ["mcp"]
    }
  }
}
EOF
  ok "Created $MCP_CONFIG with pgmemory MCP server"
fi

# ── Claude Desktop MCP config ──────────────────────────────────────

CLAUDE_CONFIG_DIR="$HOME/Library/Application Support/Claude"
CLAUDE_CONFIG="$CLAUDE_CONFIG_DIR/claude_desktop_config.json"

if [[ "$(uname)" == "Darwin" && -d "$CLAUDE_CONFIG_DIR" ]]; then
  if [[ -f "$CLAUDE_CONFIG" ]]; then
    if grep -q '"pgmemory"' "$CLAUDE_CONFIG" 2>/dev/null; then
      ok "pgmemory already in Claude Desktop config"
    else
      info "Adding pgmemory to Claude Desktop config"
      python3 -c "
import json
with open('$CLAUDE_CONFIG') as f:
    cfg = json.load(f)
servers = cfg.setdefault('mcpServers', {})
servers.pop('memoryd', None)  # remove legacy entry
servers['pgmemory'] = {
    'command': '$MEMORYD_BIN',
    'args': ['mcp']
}
with open('$CLAUDE_CONFIG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null && ok "Added pgmemory to Claude Desktop config" || info "Could not update Claude Desktop config — add manually"
    fi
  fi
fi

# ── Start everything ───────────────────────────────────────────────

step "Starting pgmemory"

# Stop any existing daemon.
if curl -sf "http://127.0.0.1:7432/health" >/dev/null 2>&1; then
  info "Stopping existing daemon..."
  curl -sf -X POST "http://127.0.0.1:7432/shutdown" >/dev/null 2>&1 || true
  sleep 1
fi

if [[ "$(uname)" == "Darwin" && -d "$SCRIPT_DIR/bin/Pgmemory.app" ]]; then
  pkill -f "Pgmemory.app" 2>/dev/null || true
  sleep 0.5
  open "$SCRIPT_DIR/bin/Pgmemory.app"
  ok "Pgmemory.app launched (menu bar + daemon)"
else
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
else
  fail "Daemon did not start — check $MEMORYD_DIR/daemon.log"
fi

# ── Done ────────────────────────────────────────────────────────────

step "Ready!"

echo ""
echo "  pgmemory is running and ready to use."
echo ""
echo "  Dashboard:     http://127.0.0.1:7432"
echo "  Proxy mode:    export ANTHROPIC_BASE_URL=http://127.0.0.1:7432"
echo "  MCP mode:      Already configured in ~/.mcp.json"
echo ""
if [[ "$(uname)" == "Darwin" ]]; then
echo "  The Pgmemory menu bar app is running — look for M● in your menu bar."
echo "  Use it to start/stop the daemon, switch modes, and manage sources."
echo ""
fi
