#!/bin/bash
# dev-setup.sh — Set up a development BBS instance from a git checkout
#
# Creates a working BBS directory tree at a target path, populated with
# template configs, menus, data skeleton, and scripts.  Optionally symlinks
# binaries back to the git repo so the BBS always runs the latest build.
#
# Usage:
#   ./dev-setup.sh <target-dir> [--symlink]
#
# Examples:
#   ./dev-setup.sh ~/bbs-test              # copy binaries
#   ./dev-setup.sh ~/bbs-test --symlink    # symlink binaries to git repo
#   ./dev-setup.sh ~/bbs-dev --symlink     # second instance, different ports

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}==>${NC} $*"; }
warn()  { echo -e "${YELLOW}==>${NC} $*"; }
fail()  { echo -e "${RED}ERROR:${NC} $*" >&2; exit 1; }

# ── Parse arguments ──────────────────────────────────────────────
SYMLINK=0
TARGET=""

for arg in "$@"; do
  case "$arg" in
    --symlink) SYMLINK=1 ;;
    --help|-h)
      echo "Usage: $0 <target-dir> [--symlink]"
      echo ""
      echo "Sets up a development BBS instance from this git checkout."
      echo ""
      echo "Options:"
      echo "  --symlink   Symlink binaries to git repo instead of copying"
      echo "              (BBS always uses latest 'go build' output)"
      echo ""
      echo "After setup, build binaries and start the BBS:"
      echo "  cd <target-dir> && ./vision3"
      exit 0
      ;;
    -*)
      fail "Unknown option: $arg"
      ;;
    *)
      if [[ -n "$TARGET" ]]; then
        fail "Multiple target directories specified"
      fi
      TARGET="$arg"
      ;;
  esac
done

if [[ -z "$TARGET" ]]; then
  echo "Usage: $0 <target-dir> [--symlink]"
  echo "Run '$0 --help' for details."
  exit 1
fi

# Resolve paths
REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$(realpath -m "$TARGET")"

if [[ "$TARGET" == "$REPO_DIR" ]]; then
  fail "Target directory cannot be the git repo itself"
fi

echo "=== ViSiON/3 Dev Setup ==="
echo "  Source repo: $REPO_DIR"
echo "  Target dir:  $TARGET"
if [[ $SYMLINK -eq 1 ]]; then
  echo "  Binaries:    symlinked to repo"
else
  echo "  Binaries:    copied"
fi
echo ""

# ── Prerequisites ─────────────────────────────────────────────────
command -v go >/dev/null 2>&1 || fail "Go is not installed (https://golang.org/dl/)"
command -v ssh-keygen >/dev/null 2>&1 || fail "ssh-keygen is not installed"

# ── Create target directory ──────────────────────────────────────
mkdir -p "$TARGET"

# ── Build binaries first ─────────────────────────────────────────
BINARIES=(vision3 helper v3mail strings ue config menuedit)

info "Building binaries..."
for bin in "${BINARIES[@]}"; do
  if [[ -d "$REPO_DIR/cmd/$bin" ]]; then
    echo "  Building $bin..."
    (cd "$REPO_DIR" && go build -o "$bin" "./cmd/$bin")
  fi
done
echo ""

# ── Install binaries ─────────────────────────────────────────────
info "Installing binaries..."
for bin in "${BINARIES[@]}"; do
  src="$REPO_DIR/$bin"
  dst="$TARGET/$bin"
  [[ -f "$src" ]] || continue

  if [[ $SYMLINK -eq 1 ]]; then
    ln -sf "$src" "$dst"
    echo "  $bin -> $src"
  else
    cp "$src" "$dst"
    chmod +x "$dst"
    echo "  $bin (copied)"
  fi
done
echo ""

# ── Create directory structure ───────────────────────────────────
info "Creating directory structure..."
mkdir -p "$TARGET"/{configs,bin,scripts}
mkdir -p "$TARGET"/data/{users,files/general,logs,msgbases}
mkdir -p "$TARGET"/data/ftn/{in,secure_in,temp_in,temp_out,out,dupehist,dloads,dloads/pass}
mkdir -p "$TARGET"/data/infoforms/{templates,responses}
mkdir -p "$TARGET"/doors/drive_c

# ── Copy template configs ────────────────────────────────────────
info "Setting up configuration files..."
for f in "$REPO_DIR"/templates/configs/*.json; do
  [[ -f "$f" ]] || continue
  dst="$TARGET/configs/$(basename "$f")"
  if [[ ! -f "$dst" ]]; then
    echo "  Creating $(basename "$f") from template..."
    cp "$f" "$dst"
  else
    echo "  $(basename "$f") already exists, skipping."
  fi
done

for f in "$REPO_DIR"/templates/configs/*.txt; do
  [[ -f "$f" ]] || continue
  dst="$TARGET/configs/$(basename "$f")"
  if [[ ! -f "$dst" ]]; then
    cp "$f" "$dst"
  fi
done

# sexyz.ini
if [[ -f "$REPO_DIR/templates/configs/sexyz.ini" ]] && [[ ! -f "$TARGET/bin/sexyz.ini" ]]; then
  cp "$REPO_DIR/templates/configs/sexyz.ini" "$TARGET/bin/sexyz.ini"
fi

# binkd.conf
if [[ -f "$REPO_DIR/templates/configs/binkd.conf" ]] && [[ ! -f "$TARGET/data/ftn/binkd.conf" ]]; then
  cp "$REPO_DIR/templates/configs/binkd.conf" "$TARGET/data/ftn/binkd.conf"
fi

# ── Copy menus ───────────────────────────────────────────────────
if [[ -d "$REPO_DIR/menus" ]] && [[ ! -d "$TARGET/menus" ]]; then
  info "Copying menus..."
  cp -r "$REPO_DIR/menus" "$TARGET/menus"
elif [[ -d "$TARGET/menus" ]]; then
  info "Menus directory already exists, skipping."
fi

# ── Copy scripts ─────────────────────────────────────────────────
if [[ -d "$REPO_DIR/scripts/examples" ]] && [[ ! -d "$TARGET/scripts/examples" ]]; then
  info "Copying example scripts..."
  cp -r "$REPO_DIR/scripts/examples" "$TARGET/scripts/examples"
fi

# ── Infoforms ────────────────────────────────────────────────────
if [[ -f "$REPO_DIR/templates/infoforms/config.json" ]] && [[ ! -f "$TARGET/data/infoforms/config.json" ]]; then
  cp "$REPO_DIR/templates/infoforms/config.json" "$TARGET/data/infoforms/config.json"
fi
for f in "$REPO_DIR"/templates/infoforms/form_*.txt; do
  [[ -f "$f" ]] || continue
  dst="$TARGET/data/infoforms/templates/$(basename "$f")"
  [[ -f "$dst" ]] || cp "$f" "$dst"
done

# ── SSH host key ─────────────────────────────────────────────────
if [[ ! -f "$TARGET/configs/ssh_host_rsa_key" ]]; then
  info "Generating SSH host key..."
  ssh-keygen -t rsa -b 4096 -f "$TARGET/configs/ssh_host_rsa_key" -N "" -q
fi

# ── Initial data files ───────────────────────────────────────────
if [[ ! -f "$TARGET/data/oneliners.json" ]]; then
  echo "[]" > "$TARGET/data/oneliners.json"
fi

if [[ ! -f "$TARGET/data/users/users.json" ]]; then
  info "Creating default sysop account..."
  cat > "$TARGET/data/users/users.json" << 'EOF'
[
  {
    "id": 1,
    "username": "felonius",
    "passwordHash": "$2a$10$4BzeQ5Pgg6GT6ckfLtTJOuInTvQxXRSj0DETBGIL87SYG2hHpXbtO",
    "handle": "Felonius",
    "accessLevel": 255,
    "flags": "",
    "lastLogin": "0001-01-01T00:00:00Z",
    "timesCalled": 0,
    "lastBulletinRead": "0001-01-01T00:00:00Z",
    "realName": "Joe Sysop",
    "createdAt": "2024-01-01T00:00:00Z",
    "validated": true,
    "filePoints": 0,
    "numUploads": 0,
    "timeLimit": 60,
    "privateNote": "SysOp",
    "current_msg_conference_id": 1,
    "current_msg_conference_tag": "LOCAL",
    "current_file_conference_id": 1,
    "current_file_conference_tag": "LOCAL",
    "group_location": "ViSiON/3",
    "current_message_area_id": 1,
    "current_message_area_tag": "GENERAL",
    "current_file_area_id": 1,
    "current_file_area_tag": "GENERAL",
    "screenWidth": 80,
    "screenHeight": 24
  }
]
EOF
fi

if [[ ! -f "$TARGET/data/users/callhistory.json" ]]; then
  echo "[]" > "$TARGET/data/users/callhistory.json"
fi

if [[ ! -f "$TARGET/data/users/callnumber.json" ]]; then
  echo "1" > "$TARGET/data/users/callnumber.json"
fi

# ── Initialize JAM bases ─────────────────────────────────────────
info "Initializing JAM message bases..."
(cd "$TARGET" && ./v3mail stats --all --config configs --data data > /dev/null 2>&1) || true

# ── Done ─────────────────────────────────────────────────────────
echo ""
echo "=== Dev Setup Complete ==="
echo ""
echo "  Target: $TARGET"
echo "  Login:  felonius / password"
echo ""
if [[ $SYMLINK -eq 1 ]]; then
  echo "Binaries are symlinked to $REPO_DIR"
  echo "After rebuilding (go build -o vision3 ./cmd/vision3), the BBS"
  echo "picks up the new binary automatically."
  echo ""
fi
echo "To start the BBS:"
echo "  cd $TARGET && ./vision3"
echo ""
echo "To connect:"
echo "  ssh user@localhost -p 2222"
echo ""
echo "Tip: To run multiple instances, edit configs/config.json in each"
echo "     target dir and change sshPort/telnetPort to avoid conflicts."
