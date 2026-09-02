# Keeping Binaries Up to Date

This guide is for sysops who want to track active development — running the latest unreleased code without downloading a new release archive each time.

> **Prerequisites:** Go 1.24+ ([install Go](https://golang.org/dl/)), Git.

---

## Option A: Dev Setup (Recommended)

The `dev-setup.sh` script creates a complete BBS installation from a git checkout. With `--symlink`, rebuilding in the repo automatically updates the running BBS.

### 1. Clone the repository

```bash
git clone https://github.com/ViSiON-3/vision-3-bbs.git ~/git/vision3
cd ~/git/vision3
```

### 2. Create a BBS instance

```bash
./dev-setup.sh /opt/vision3 --symlink
```

This builds all binaries, creates the full directory tree (configs, data, menus, etc.), generates an SSH host key, and symlinks each binary back to the repo.

### 3. Start the BBS

```bash
cd /opt/vision3
./vision3
# Connect: ssh felonius@localhost -p 2222
# Default login: felonius / password
```

### 4. Update workflow

Pull and rebuild — the symlinks mean the BBS immediately has the new binaries:

```bash
cd ~/git/vision3
git pull && ./build.sh
```

Restart the BBS to pick up the new binaries.

---

## Option B: Symlink into an Existing Release Installation

If you already have a working BBS from a release archive and want to switch to tracking source:

```bash
# Clone and build
git clone https://github.com/ViSiON-3/vision-3-bbs.git ~/git/vision3
cd ~/git/vision3
./build.sh

# Replace binaries with symlinks
cd /opt/vision3
for bin in vision3 helper v3mail strings ue config menuedit; do
    [ -f "$bin" ] && mv "$bin" "$bin.bak"
    ln -sf ~/git/vision3/$bin ./$bin
done
```

Your `configs/`, `data/`, `menus/`, and `bin/` directories remain untouched — only the Go binaries are replaced.

### Menu and art fixes are not picked up automatically

That last point cuts both ways. Because `menus/` is never overwritten, a release
that fixes a menu screen, a lightbar layout or a command binding **will not reach
your board from `git pull` alone** — you rebuild, restart, and the old screen is
still there with nothing to indicate anything was missed. `dev-setup.sh` copies
`menus/` only when the target has none, and says "Menus directory already exists,
skipping" otherwise.

After pulling, check whether the release touched anything under `menus/` and copy
those files across by hand:

```bash
# from your repo checkout, see what changed since the version you were on
git diff --stat <your-old-tag>..HEAD -- menus/

# copy an individual fixed file into your instance
cp menus/v3/ansi/SOMEMENU.ANS  /opt/vision3/menus/v3/ansi/
cp menus/v3/cfg/SOMEMENU.CFG   /opt/vision3/menus/v3/cfg/
cp menus/v3/bar/SOMEMENU.BAR   /opt/vision3/menus/v3/bar/
```

A screen's files travel together — a fix may span the `.ANS` art, its `.BAR`
lightbar coordinates and its `.CFG` key bindings, so copy whatever the release
changed as a set; taking one file and not the others can leave you worse off
than before. (For most menus the three must agree outright; menus that read
their own keys in Go, like the Sponsor Menu, keep only a subset in their `.CFG`
— see [the menu system guide](menus/menu-system.md#keeping-the-bar-file-and-the-art-in-step).)
If you have customised a file, diff it against the repo's version rather than
overwriting it.

This does not apply if you run Vision/3 directly out of the git checkout, or if
you bind-mount the repo's `menus/` as `docker-compose.yml` does — there `git pull`
is enough.

---

## Running Multiple Instances

Use `dev-setup.sh` to create additional BBS instances for testing:

```bash
./dev-setup.sh ~/bbs-test1 --symlink
./dev-setup.sh ~/bbs-test2 --symlink
```

Edit `configs/config.json` in each instance to use different ports:

```json
{
  "sshPort": 2223,
  "telnetPort": 2324
}
```

Both instances share the same binaries (via symlinks) but have independent configs and data.

---

## Tracking a Specific Branch

```bash
cd ~/git/vision3
git fetch
git checkout feature/some-branch
./build.sh
```

Switch back to main at any time with `git checkout main && ./build.sh`.

---

## Notes

- **Data and configs are not affected.** Symlinks only replace the Go binaries.
- **`bin/` binaries (`sexyz`, `binkd`) are not built from Go source.** If you need these, download them from a [release archive](https://github.com/ViSiON-3/vision-3-bbs/releases) and copy them into your instance's `bin/` directory.
- **Windows users** can use `dev-setup.sh` under WSL, or copy binaries manually after each build. Symlinks on Windows require Developer Mode.
- **After major updates**, check release notes for new config keys or migration steps.
