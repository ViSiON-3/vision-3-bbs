# Keeping Binaries Up to Date

This guide is for sysops who want to track active development — running the latest unreleased code without downloading a new release archive each time.

The approach: keep a cloned copy of the repo for building, and symlink the binaries in your release installation to the freshly-built output. When you rebuild from source, the running installation picks up the new binaries automatically.

> **Prerequisites:** Go 1.24+ ([install Go](https://golang.org/dl/)), Git, and a working release installation (step 1 below).

---

## Step 1: Install from a Release Archive

Start with a release bundle — this gives you all the runtime assets (configs, menus, data, Synchronet JS libraries, sexyz, binkd) that the source repo alone does not provide.

```bash
mkdir -p /opt/vision3
tar -xzf vision3-bundle-linux-amd64-v*.tar.gz -C /opt/vision3
cd /opt/vision3
./setup.sh
```

Verify the BBS starts and works before continuing:

```bash
./vision3
# Connect: ssh felonius@localhost -p 2222
```

---

## Step 2: Clone the Repository

Pick a location for the source tree. This is separate from your BBS installation directory.

```bash
git clone https://github.com/ViSiON-3/vision-3-bbs.git ~/git/vision3
cd ~/git/vision3
./build.sh
```

This produces the Go binaries in the repo root: `vision3`, `helper`, `v3mail`, `strings`, `ue`, `config`, `menuedit`.

---

## Step 3: Replace Binaries with Symlinks

Stop the BBS, then replace each binary in your installation directory with a symlink to the repo's build output:

```bash
cd /opt/vision3

# Back up originals (recommended the first time)
mkdir -p backups
for bin in vision3 helper v3mail strings ue config menuedit; do
    [ -f "$bin" ] && mv "$bin" backups/
done

# Create symlinks
for bin in vision3 helper v3mail strings ue config menuedit; do
    ln -sf ~/git/vision3/$bin ./$bin
done
```

Verify:

```bash
ls -la vision3 helper v3mail strings ue config menuedit
# Each should show -> ~/git/vision3/<binary>
```

---

## Step 4: Update Workflow

From now on, pulling updates and rebuilding is two commands:

```bash
cd ~/git/vision3
git pull && ./build.sh
```

Because of the symlinks, your installation immediately has the new binaries. Restart the BBS to pick them up.

> **Tip:** Wrap the pull-build-restart cycle into a small script — see [Example: Auto-Update Script](#example-auto-update-script) below.

---

## Example: Auto-Update Script

Save as `~/bin/v3update.sh` and `chmod +x` it:

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

By default `git pull` tracks `main`. To follow a development or feature branch:

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

- **Data and configs are not affected.** Symlinks only replace the Go binaries. Your `configs/`, `data/`, `menus/`, `doors/`, and `bin/` directories remain untouched.
- **`bin/` binaries (`sexyz`, `binkd`) are not built from Go source.** Keep the copies from your release archive — the symlink approach only applies to the Go binaries.
- **Windows users** can use a similar approach by copying binaries after each build. Symlinks on Windows require elevated privileges or Developer Mode enabled.
- **After major updates**, check the release notes or commit log for new config keys or migration steps. Occasionally a new release extraction may be needed if new template files or directories are introduced.
