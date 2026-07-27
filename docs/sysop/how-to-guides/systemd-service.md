# Running ViSiON/3 as a systemd Service

On Linux, running the BBS under systemd means it starts automatically at
boot, restarts if it ever crashes, and logs to the journal alongside the
regular `data/logs/vision3.log` file.

This guide assumes you installed a release bundle to `/opt/vision3` as
described in the [Installation Guide](getting-started/installation.md).
Adjust paths if you installed elsewhere.

> **Note:** If you use the integrated binkd mailer (Server Setup →
> `binkd` enabled), you do **not** need a separate unit for binkd —
> ViSiON/3 starts, supervises, and stops it as part of its own lifecycle.

---

## Step 1: Create a Dedicated User (Recommended)

Run the BBS as an unprivileged user rather than root:

```bash
sudo useradd --system --home-dir /opt/vision3 --shell /usr/sbin/nologin bbs
sudo chown -R bbs:bbs /opt/vision3
```

With the files owned by `bbs`, you'll run the TUI editors via
`sudo -u bbs` — see [Running the TUI Editors](#running-the-tui-editors)
below.

## Step 2: Create the Unit File

Copy the template below to `/etc/systemd/system/vision3.service`:

```ini
[Unit]
Description=ViSiON/3 BBS
Documentation=https://github.com/ViSiON-3/vision-3-bbs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=bbs
Group=bbs

# IMPORTANT: vision3 resolves configs/, data/, and menus/ relative to its
# working directory. This must be your BBS install directory.
WorkingDirectory=/opt/vision3
ExecStart=/opt/vision3/vision3

# vision3 shuts down cleanly on SIGTERM (systemd's default stop signal):
# the mailer, scheduler, and node sessions all get a chance to close.
Restart=on-failure
RestartSec=5

# Basic hardening
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/opt/vision3

[Install]
WantedBy=multi-user.target
```

If you run the BBS as a different user or from a different directory,
change `User=`, `Group=`, `WorkingDirectory=`, `ExecStart=`, and
`ReadWritePaths=` to match.

## Step 3: Enable and Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now vision3
```

Check that it came up:

```bash
systemctl status vision3
```

You should see `active (running)` and the SSH/Telnet listener lines in the
log output.

## Step 4: Verify a Connection

```bash
ssh felonius@localhost -p 2222
```

---

## Viewing Logs

ViSiON/3 writes to `data/logs/vision3.log` and echoes the same output to
stderr, which systemd captures in the journal:

```bash
journalctl -u vision3 -f          # follow live
journalctl -u vision3 --since today
```

## Managing the Service

```bash
sudo systemctl stop vision3       # graceful shutdown (SIGTERM)
sudo systemctl restart vision3    # e.g. after changing config or binaries
sudo systemctl disable vision3    # stop starting at boot
```

Restart the service after editing configuration with `./config` or after
updating binaries (see
[Keeping Binaries Updated](how-to-guides/keeping-binaries-updated.md)).

---

## Running the TUI Editors

With the install owned by the `bbs` user, the TUI editors (`config`, `ue`,
`strings`, `menuedit`, `helper`) can't save changes when run from your own
login — they write directly to `configs/`, `data/`, and `menus/`. Run them
as the `bbs` user instead:

```bash
cd /opt/vision3
sudo -u bbs ./config
sudo -u bbs ./ue
```

TUI programs work normally under `sudo -u` — they still use your terminal.

The `wfc` console is the exception: it connects to the BBS over SSH rather
than touching files, so it runs from any account without sudo.

If you'd rather not deal with `sudo` at all, the alternative is to skip the
dedicated user and set `User=`/`Group=` in the unit to your own login
account. You then own all the files and run the editors directly. The
trade-off: the network-facing BBS process runs with your account's
privileges instead of an unprivileged system user's.

Either way, remember that changes made with `./config` while the service is
running take effect only after `sudo systemctl restart vision3` — and the
restart disconnects any connected callers.

---

## Running from Source (Symlinked Binaries)

If you set up the BBS with `dev-setup.sh /opt/vision3 --symlink` (see
[Keeping Binaries Updated](how-to-guides/keeping-binaries-updated.md)), the
binaries in the install directory are symlinks into your repo under your
home directory — and two settings in the template above will break exec
with `status=203/EXEC`:

- `ProtectHome=true` hides all of `/home` from the service, so systemd
  can't follow the symlink to its target — even though the link looks fine
  from your shell.
- The dedicated `bbs` user can't traverse your home directory (typically
  mode `700`/`750`) to reach the target anyway.

For a symlinked from-source setup, run the service as your own account and
relax `ProtectHome` to read-only. In the `[Service]` section:

```ini
User=youruser
Group=youruser
ProtectHome=read-only
```

**Replace** the existing `ProtectHome=true` line in the hardening block —
don't just add a second `ProtectHome=` entry. When a directive appears
twice in a unit, systemd silently uses the last one, so a leftover
`ProtectHome=true` further down will keep overriding `read-only` and the
service will keep failing with `203/EXEC`.

`read-only` still blocks writes to `/home` but allows read and exec, which
is all the symlinked binaries need — the BBS writes only under the install
directory, which stays writable via `ReadWritePaths`. Running as your own
account also means you own all the files, so the TUI editors work without
`sudo`.

Note that rebuilding (`./build.sh`) replaces the binary in the repo, but
the running service keeps executing the old code until you
`sudo systemctl restart vision3`.

---

## Using Privileged Ports (22 / 23)

The default ports (2222 SSH, 2323 Telnet) work without extra privileges.
If you want the BBS to listen on ports below 1024 (e.g. 22 or 23) while
still running as an unprivileged user, add this to the `[Service]` section:

```ini
AmbientCapabilities=CAP_NET_BIND_SERVICE
```

Then set the ports in `./config` → System Configuration → Server Setup and
restart the service.

---

## Troubleshooting

### Service exits immediately

Check the journal for the actual error:

```bash
journalctl -u vision3 -n 50
```

The most common cause is a wrong `WorkingDirectory=` — the BBS must be
started from the install directory so it can find `configs/`, `data/`, and
`menus/`.

### `status=203/EXEC`

systemd could not execute the binary at all — the failure happens before
ViSiON/3 ever runs. Check, in order:

1. The `ExecStart=` path is correct and the binary exists there.
2. The service user can execute it — test with
   `sudo -u bbs /opt/vision3/vision3 --help` (prints usage without starting
   the server). If that fails with permission denied, fix `chmod +x` on the
   binary or directory permissions along the path.
3. The binary is not a symlink into your home directory while the unit has
   `ProtectHome=true` — see
   [Running from Source](#running-from-source-symlinked-binaries) above.
4. The binary matches your CPU architecture (`file /opt/vision3/vision3`).
5. On SELinux systems (Fedora/RHEL), try `restorecon -rv /opt/vision3`.

### Permission errors

Make sure the service user owns the entire install directory:

```bash
sudo chown -R bbs:bbs /opt/vision3
```

### Port already in use

Another service (often the system's own sshd on port 22) is bound to the
configured port. Pick a different port in Server Setup or stop the
conflicting service.
