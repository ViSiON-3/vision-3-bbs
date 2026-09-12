# Docker Deployment Guide for ViSiON/3

This guide covers deploying ViSiON/3 using Docker and Docker Compose.

## Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+ (optional, but recommended)

## Quick Start with Docker Compose

1. **Clone the repository:**

   ```bash
   git clone https://github.com/ViSiON-3/vision-3-bbs.git
   cd vision-3-bbs
   ```

2. **Start the BBS:**

   ```bash
   docker compose up -d
   ```

   This will:
   - Build the Docker image
   - Compile all Go binaries (ViSiON3, v3mail, helper, strings, ue, config, menuedit, wfc)
   - Create necessary directories (`configs/`, `data/`, `temp/`)
   - Generate SSH host keys automatically
   - Initialize config files from templates
   - Start the BBS on ports 2222 (SSH) and 2323 (telnet)

   > **sexyz and binkd are not bundled.** The image ships `sexyz.ini` but not the
   > binaries themselves — see [Adding sexyz and binkd](#adding-sexyz-and-binkd).

3. **Check logs:**

   ```bash
   docker compose logs -f
   ```

4. **Connect to the BBS:**

   ```bash
   ssh felonius@localhost -p 2222
   # Default password: password
   ```

## Manual Docker Build

If you prefer not to use Docker Compose:

1. **Build the image:**

   ```bash
   docker build -t vision3:latest .
   ```

2. **Create host directories:**

   ```bash
   mkdir -p configs data
   ```

3. **Run the container:**

   ```bash
   docker run -d \
     --name vision3-bbs \
     -p 2222:2222 \
     -p 2323:2323 \
     -v "$(pwd)/configs:/vision3/configs" \
     -v "$(pwd)/data:/vision3/data" \
     vision3:latest
   ```

   The default menu set ships inside the image, so no `menus/` mount is needed.
   To customise menus, mount an overlay directory instead of the whole set:
   `-v "$(pwd)/menus.d:/vision3/menus.d"`. Files in it are read before the
   built-in ones, file by file, and `menuedit` inside the container saves there
   (see [Customising menus without losing your changes](../menus/menu-system.md#customising-menus-without-losing-your-changes)).
   Add `-v "$(pwd)/menus:/vision3/menus"` only if you keep a complete set of
   your own on the host — mounting an *empty* directory there hides the
   built-in menus and the pre-flight check will refuse to start.

## Important Notes

### Docker Image Notes

ViSiON/3 uses a pure-Go SSH implementation (`gliderlabs/ssh`) — no CGO or native libraries required. The Dockerfile:

- Builds all Go binaries: `ViSiON3`, `v3mail`, `config`, `strings`, `ue`, `menuedit`, `helper`, `wfc`
- Ships the default menu set at `/vision3/menus`
- Ships `sexyz.ini`, which the entrypoint copies to `bin/`

The builder stage is pinned to the Go version in `go.mod`. The official `golang`
images set `GOTOOLCHAIN=local`, so if `go.mod` is bumped past the base image the
build fails at `go mod download` rather than fetching a newer toolchain — keep
the two in step.

### Adding sexyz and binkd

The `sexyz` (ZModem 8k file transfers) and `binkd` (FTN mailer) binaries are
**not** in the image: they are third-party builds that must match the
container's architecture (linux/amd64 for the Alpine base). Supply them with a
mount:

```yaml
volumes:
  - ./bin:/vision3/bin
```

or add them in a derived image:

```dockerfile
FROM vision3:latest
COPY bin/sexyz bin/binkd /vision3/bin/
```

`.dockerignore` excludes `bin/` but re-includes those two paths specifically, so
the `COPY` resolves with the repository root as the build context.

See [File Transfer Protocols](files/file-transfer.md) for build instructions.

### Container user and file ownership

The BBS runs as the unprivileged `vision3` user (uid 100, gid 101). The
entrypoint starts as root only long enough to `chown` the mounted volumes, then
drops privileges — so Docker-created bind mounts, which arrive owned by root,
work without any manual preparation.

Because of that privilege drop, `docker exec` lands you as **root**, not
`vision3`. Always pass `-u vision3` when running the TUI tools, or they will
leave root-owned files in `configs/` that the BBS cannot rewrite.

`menus/` is deliberately left alone. Compose bind-mounts your checkout there, and
taking ownership of it would leave you unable to `git pull` or edit your own menu
set on the host. The BBS only reads menus, so a normal checkout works as-is.

`menus.d/` — the overlay your customisations go in — **is** chowned like
`configs/` and `data/`, because `menuedit` in the container writes there. Git
tracks nothing in it but a README, so the ownership change costs the host
nothing. That is what makes editing menus from inside the container work:

```bash
docker compose exec -u vision3 vision3 ./menuedit   # saves into /vision3/menus.d
```

Edits made on the host land in the same directory (`./menus.d`) and are picked
up on the next menu load. You never need to make `menus/` writable. If you
customised `menus/` before the overlay existed, move those files across once —
see [Moving existing customisations into menus.d](../menus/menu-system.md#moving-existing-customisations-into-menusd).

### Persistent Data

The following directories are mounted as volumes and persist across container restarts:

- **`configs/`** - Configuration files (created from templates on first run)
  - `config.json` - Main BBS configuration (ports, security levels, connection limits)
  - `message_areas.json` - Message area definitions (includes PRIVMAIL)
  - `file_areas.json` - File area definitions
  - `doors.json` - Door configurations
  - `ssh_host_rsa_key` - SSH host key (auto-generated)
  - Other config files

- **`data/`** - Runtime data
  - `users/` - User database and call history
  - `msgbases/` - JAM message bases (including `privmail/`)
  - `files/` - File areas
  - `ftn/` - FidoNet/echomail data
  - `logs/` - Application logs (vision3.log, v3mail.log, binkd.log)

- **`menus/`** - The shipped menu set (ANSI screens, configs)
  - Mount your checkout here so `git pull` updates it, or leave it to the image

- **`menus.d/`** - Your menu overrides
  - Read before `menus/`, file by file; `menuedit` saves here

### First Run Initialization

On first run, the entrypoint script will:

1. Fix ownership of the mounted volumes, then drop to the `vision3` user
2. Create necessary directories
3. Generate SSH host keys (RSA and ED25519)
4. Copy template configs and IP list files to `configs/` if missing
5. Seed `data/oneliners.json` and the call-history counters
6. Initialize the JAM message bases

The default sysop account (`felonius` / `password`, access level 255) is created
by the BBS itself on first start, not by the entrypoint — watch for the
`created default sysop account` warning in the logs.

### Configuration

After first run, use the TUI tools to configure your BBS. Run them via `docker exec`:

```bash
# Main config editor (BBS name, ports, access levels, networking, etc.)
docker exec -u vision3 -it vision3-bbs ./config

# String editor (display text and prompts)
docker exec -u vision3 -it vision3-bbs ./strings

# User editor
docker exec -u vision3 -it vision3-bbs ./ue

# Menu editor
docker exec -u vision3 -it vision3-bbs ./menuedit

# WFC sysop console
docker exec -u vision3 -it vision3-bbs ./wfc
```

> Omitting `-u vision3` runs the tool as root and writes root-owned files into
> the mounted volumes, which the BBS itself (running as `vision3`) then cannot
> modify.

The container's working directory is `/vision3`, which is where `configs/`, `data/`, and `menus/` are mounted — so the TUI tools find your files automatically without extra flags.

After saving changes in the TUI, restart to apply most settings:

```bash
docker compose restart
```

Exception: IP blocklist/allowlist files are watched and reload automatically — no restart needed.

## Private Mail Setup

The PRIVMAIL area is automatically configured in `configs/message_areas.json`. The Docker setup ensures:

- `data/msgbases/privmail/` directory is created
- JAM message base files are initialized on first message
- EMAILM menu is accessible via the E key from main menu

## Updating

To update to the latest version:

```bash
# Pull latest code
git pull

# Rebuild and restart
docker compose up -d --build
```

Your data in `configs/`, `data/`, and `menus/` volumes will be preserved.

## Troubleshooting

### SSH Connection Refused

If you can't connect via SSH:

1. Check container logs: `docker compose logs`
2. Verify port 2222 is not already in use: `netstat -ln | grep 2222`
3. Check SSH keys were generated: `ls -l configs/ssh_host_*`

### Configuration Not Loading

If config changes aren't applied:

1. Ensure config files exist in the `configs/` volume
2. Restart the container: `docker compose restart`
3. Check file permissions (should be readable by container)

### Message Base Errors

If you see JAM-related errors:

1. Check directory permissions in `data/msgbases/`
2. Ensure PRIVMAIL area exists in `configs/message_areas.json`
3. Delete corrupted JAM files and let them regenerate

## Advanced Usage

### Custom Ports

To use different ports, edit `docker-compose.yml`:

```yaml
ports:
  - "2323:2222" # Expose SSH on port 2323
  - "2324:2323" # Expose telnet on port 2324
```

### Resource Limits

Uncomment the `deploy` section in `docker-compose.yml` to set CPU/memory limits.

### Multiple Nodes

To run multiple BBS nodes:

```yaml
services:
  vision3-node1:
    # ... same config, different port
    ports:
      - "2222:2222"

  vision3-node2:
    # ... same config, different port
    ports:
      - "2223:2222"
```

### Custom Menu Set

To override individual files, put them in `./menus.d` (mounted by default):

```
menus.d/v3/ansi/MAIN.ANS   # replaces the shipped MAIN.ANS; everything else is unchanged
```

To replace the whole set, mount a complete menu directory over the shipped one:

```yaml
volumes:
  - ./my-custom-menus:/vision3/menus
```

## Production Deployment

For production deployments:

1. **Use a reverse proxy** (nginx, Caddy) for SSH multiplexing if needed
2. **Set up backups** for the `data/` volume
3. **Monitor logs** with a logging solution (ELK, Loki, etc.)
4. **Set resource limits** to prevent runaway processes
5. **Use Docker secrets** for sensitive configuration
6. **Enable auto-restart**: `restart: unless-stopped` (already set)

## Security Considerations

- Change the default password immediately after first login
- Use strong SSH host keys (automatically generated)
- Keep the Docker image updated
- Limit exposed ports (only expose 2222)
- Consider running with a non-root user inside the container
- Set up firewall rules on the host
- Configure connection limits via `docker exec -it vision3-bbs ./config` → System Configuration → Connection Limits (Max Nodes, Max Per IP, failed login lockout)
- Configure IP filtering via System Configuration → IP Blocklist/Allowlist (paths to plain-text files with one IP or CIDR per line)

## Support

For issues related to Docker deployment:

- Check logs: `docker compose logs -f`
- GitHub Issues: <https://github.com/ViSiON-3/vision-3-bbs/issues>
- Include Docker version, OS, and error logs when reporting issues
