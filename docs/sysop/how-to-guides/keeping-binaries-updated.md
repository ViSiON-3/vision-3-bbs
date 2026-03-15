# Keeping Binaries Up to Date

If you're running ViSiON/3 while tracking active development, this guide shows how to quickly pull updates and get them running without re-extracting release archives each time.

---

## Strategy: Symlink from Source

The idea is simple: keep a cloned copy of the repo for building, and point your BBS installation at the freshly built binaries using symbolic links. When you rebuild from source, your running installation picks up the new binaries automatically.

### Prerequisites

- **Go 1.24+** — required to build from source ([install Go](https://golang.org/dl/))
- **Git**
- A working ViSiON/3 installation (from a [release archive](https://github.com/ViSiON-3/vision-3-bbs/releases) or a prior `setup.sh` run)

---

### 1. Clone the Repository

Pick a location for the source tree. This is where you'll pull updates and build.

```bash
git clone https://github.com/ViSiON-3/vision-3-bbs.git ~/git/vision3
cd ~/git/vision3
./build.sh
```

This produces the binaries in the repo root: `vision3`, `helper`, `v3mail`, `strings`, `ue`, `config`, `menuedit`.

### 2. Set Up Your BBS Installation

If you haven't already, download a release archive and extract it to the location you want to run the BBS from:

```bash
# Example locations — pick wherever works for you
mkdir -p /opt/vision3        # system-wide
# or
mkdir -p ~/vision3           # user home directory

cd /opt/vision3              # (or ~/vision3)
tar -xzf ~/Downloads/vision3_linux_amd64.tar.gz --strip-components=1
./setup.sh
```

Verify the BBS starts and works before proceeding:

```bash
./vision3
# Connect: ssh felonius@localhost -p 2222
```

### 3. Replace Binaries with Symlinks

Stop the BBS, then replace each binary in your installation directory with a symlink pointing to the cloned repo:

```bash
cd /opt/vision3   # your BBS installation directory

# Stop the BBS first if it's running

# Back up originals (optional but recommended the first time)
mkdir -p backups
for bin in vision3 helper v3mail strings ue config menuedit; do
    [ -f "$bin" ] && mv "$bin" backups/
done

# Create symlinks to the build output in your source tree
for bin in vision3 helper v3mail strings ue config menuedit; do
    ln -sf ~/git/vision3/$bin ./$bin
done
```

Verify the symlinks:

```bash
ls -la vision3 helper v3mail strings ue config menuedit
```

You should see each file pointing to `~/git/vision3/<binary>`.

### 4. Update Workflow

From now on, pulling updates and rebuilding is two commands:

```bash
cd ~/git/vision3
git pull && ./build.sh
```

Because of the symlinks, your BBS installation directory immediately has the new binaries. Restart the BBS to pick up the changes:

```bash
# In your BBS installation directory
./vision3
```

> **Tip:** You can wrap the pull-build-restart cycle into a small script if you do this frequently. See [Example: Auto-Update Script](#example-auto-update-script) below.

---

## Example: Auto-Update Script

Save this as `~/bin/v3update.sh` (or wherever you keep personal scripts) and `chmod +x` it:

```bash
#!/bin/bash
# v3update.sh — pull latest ViSiON/3 source and rebuild

REPO_DIR="$HOME/git/vision3"

echo "=== Updating ViSiON/3 ==="

cd "$REPO_DIR" || { echo "Error: $REPO_DIR not found"; exit 1; }

echo "Pulling latest changes..."
git pull --ff-only || { echo "Pull failed — check for local changes"; exit 1; }

echo "Building..."
./build.sh || { echo "Build failed!"; exit 1; }

echo
echo "Done! Restart the BBS to use the new binaries."
```

Usage:

```bash
v3update.sh
# Then restart the BBS
```

---

## Tracking a Specific Branch

By default `git pull` tracks `main`. If you want to follow a development or feature branch:

```bash
cd ~/git/vision3
git fetch
git checkout feature/some-branch
git pull
./build.sh
```

Switch back to main at any time:

```bash
git checkout main
git pull
./build.sh
```

---

## Notes

- **Data and configs are not affected.** Symlinks only replace the binaries. Your `configs/`, `data/`, `menus/`, and `bin/` directories in the BBS installation remain untouched.
- **Release assets like `sexyz`** are not built from the Go source. Keep the copy from your release archive in `bin/sexyz` — the symlink approach only applies to the Go binaries.
- **Windows users** can use a similar approach with directory junctions or by copying binaries after each build. Symlinks on Windows require elevated privileges or Developer Mode enabled.
- **After major updates**, check the release notes or commit log for new config keys or migration steps. Occasionally a `setup.sh` re-run may be needed if new template files or directories are introduced.
