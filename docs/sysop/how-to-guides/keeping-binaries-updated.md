# Keeping Binaries Up to Date

This guide is for sysops who want to track active development — running the latest unreleased code without downloading a new release archive each time. ViSiON/3 is pure Go, so a build is a single command and a few seconds; the trick is arranging your install so that a rebuild in the repo *is* the update, with nothing to copy and nothing of yours overwritten.

> **Prerequisites:** Go 1.25+ ([install Go](https://golang.org/dl/)), Git.
>
> For upgrading a BBS you already run — what a new version does and does not
> bring with it, and which settings you have to add by hand — see
> [Upgrading](getting-started/upgrading.md).

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

This builds all binaries (`vision3`, `helper`, `v3mail`, `strings`, `ue`, `config`, `menuedit`, `wfc`), creates the full directory tree (configs, data, menus, etc.), generates an SSH host key, creates the default sysop account, and symlinks each binary back to the repo. Existing config and data in the target are never overwritten, so it is safe to run again.

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
for bin in vision3 helper v3mail strings ue config menuedit wfc; do
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
If you have customised a file, keep your version in `menus.d/` rather than in
`menus/`: the BBS reads `menus.d/v3/ansi/SOMEMENU.ANS` in preference to the
shipped copy, so you can overwrite `menus/` freely — or symlink it to the repo
so it tracks every pull — and never lose an edit. See
[Customising menus without losing your changes](menus/menu-system.md#customising-menus-without-losing-your-changes).

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
- **`bin/` binaries (`sexyz`, `binkd`) are not built from Go source.** If you need these, download them from a [release archive](https://github.com/ViSiON-3/vision-3-bbs/releases) and copy them into your instance's `bin/` directory. To build binkd yourself, use `./scripts/build-binkd.sh` rather than compiling it by hand — a binkd built without its `configure` script has a broken MD5 and cannot authenticate CRAM-MD5 sessions ([details](messages/ftn-echomail.md#cram-md5-authentication-fails)).
- **Windows users** can use `dev-setup.sh` under WSL, or copy binaries manually after each build. Symlinks on Windows require Developer Mode.
- **After major updates**, check release notes for new config keys or migration steps.
- **`wfc` need not match the BBS.** The console talks to the daemon over SSH, so a remote co-sysop can run an older or newer build; a feature the daemon does not know yet simply shows `-` in the console.

---

## Technical Reference

### `dev-setup.sh`

```text
Usage: ./dev-setup.sh <target-dir> [--symlink]
```

| Option | Purpose |
|--------|---------|
| `--symlink` | Symlink the binaries to the git repo instead of copying them, so the instance always runs your latest `go build` output |
| `--help`, `-h` | Print usage |

The target must not be the repo root. Existing config and data in the target
are never overwritten; `menus/` is copied only when the target has none.

### `build.sh`

Builds every binary in place in the repo root, in this order: `vision3`,
`helper`, `v3mail`, `strings`, `ue`, `config`, `menuedit`, `wfc`. It stops at
the first failure.

### Cross-compiling

Every binary is pure Go (no CGO), so a build for another platform is a matter
of `GOOS`/`GOARCH`, for example `GOOS=windows GOARCH=amd64 go build -o wfc.exe ./cmd/wfc`.
The release workflow produces the bundles the same way.
