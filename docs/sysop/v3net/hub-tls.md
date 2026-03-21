# V3Net Hub TLS Setup

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice.

This guide explains how to enable TLS (HTTPS) on a V3Net hub so that
WebSocket connections between the hub and leaf nodes are encrypted.

## Overview

The V3Net hub uses a standard HTTP server for its WebSocket transport. By
default it listens on plain HTTP. When both `tlsCert` and `tlsKey` are set
in the hub configuration, the server switches to HTTPS automatically.

You have two options:

| Approach | Best For |
|----------|----------|
| **Let's Encrypt** (free, trusted) | Public hubs with a domain name |
| **Self-signed certificate** | Private networks, LAN-only hubs, or testing |

If you run a reverse proxy (nginx, Caddy, etc.) that already terminates TLS,
you can leave both fields blank and let the proxy handle encryption instead.

---

## Option 1: Let's Encrypt

Let's Encrypt issues free, publicly-trusted certificates. You need:

- A **public domain name** pointing to the hub's IP address (e.g.
  `hub.example.com`). Let's Encrypt cannot issue certificates for bare IP
  addresses or `.local` hostnames.
- Port **80** reachable from the internet (temporarily, for the ACME
  challenge during issuance and renewal).

### Linux

Install certbot:

```bash
# Debian / Ubuntu
sudo apt install certbot

# Fedora / RHEL
sudo dnf install certbot
```

Request a certificate:

```bash
sudo certbot certonly --standalone -d hub.example.com
```

Certbot writes the files to `/etc/letsencrypt/live/hub.example.com/`. Set
the V3Net hub config fields:

| Field | Value |
|-------|-------|
| `tlsCert` | `/etc/letsencrypt/live/hub.example.com/fullchain.pem` |
| `tlsKey`  | `/etc/letsencrypt/live/hub.example.com/privkey.pem` |

**Auto-renewal:** Certbot installs a systemd timer (or cron job) that renews
certificates automatically. After renewal the hub must be restarted to pick
up the new certificate:

```bash
# /etc/letsencrypt/renewal-hooks/post/restart-vision3.sh
#!/bin/sh
systemctl restart vision3
```

Make the hook executable:

```bash
sudo chmod +x /etc/letsencrypt/renewal-hooks/post/restart-vision3.sh
```

### macOS

Install certbot via Homebrew:

```bash
brew install certbot
```

Request a certificate:

```bash
sudo certbot certonly --standalone -d hub.example.com
```

The certificate files are written to `/etc/letsencrypt/live/hub.example.com/`.
Set the config fields the same as the Linux example above.

**Auto-renewal:** Homebrew does not install a renewal timer automatically.
Create a launchd plist or cron entry to run `certbot renew` periodically:

```bash
# Example cron entry (runs daily at 3 AM)
0 3 * * * /opt/homebrew/bin/certbot renew --quiet && killall -HUP vision3
```

### Windows

Use [win-acme](https://www.win-acme.com/), a free ACME client for Windows:

1. Download the latest release from
   [github.com/win-acme/win-acme/releases](https://github.com/win-acme/win-acme/releases).
2. Extract and run `wacs.exe` as Administrator.
3. Follow the interactive prompts:
   - Choose **M** (create certificate, manual input).
   - Enter your domain name (e.g. `hub.example.com`).
   - Choose the **self-hosting** validation method (uses port 80).
   - For the store step, choose **PEM files** and specify an output directory
     (e.g. `C:\certs\`).
4. win-acme writes `hub.example.com-chain.pem` and
   `hub.example.com-key.pem` to the output directory.

Set the V3Net hub config fields:

| Field | Value |
|-------|-------|
| `tlsCert` | `C:\certs\hub.example.com-chain.pem` |
| `tlsKey`  | `C:\certs\hub.example.com-key.pem` |

win-acme creates a scheduled task for automatic renewal. Add a post-renewal
script to restart the BBS after certificate updates.

---

## Option 2: Self-Signed Certificate

A self-signed certificate encrypts the connection but is not trusted by
default — leaf nodes connecting via standard HTTPS clients will reject it
unless configured to accept it. This is fine for private or testing
networks.

### Generate with OpenSSL (Linux / macOS)

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout hub-key.pem -out hub-cert.pem \
  -days 3650 -nodes \
  -subj "/CN=hub.example.com"
```

This creates a certificate valid for 10 years with no passphrase.

Set the V3Net hub config fields:

| Field | Value |
|-------|-------|
| `tlsCert` | `/path/to/hub-cert.pem` |
| `tlsKey`  | `/path/to/hub-key.pem` |

### Generate with OpenSSL (Windows)

OpenSSL is bundled with [Git for Windows](https://gitforwindows.org/). Open
Git Bash and run the same command as above, adjusting the output paths:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout C:/certs/hub-key.pem -out C:/certs/hub-cert.pem \
  -days 3650 -nodes \
  -subj "/CN=hub.example.com"
```

Set the config fields to point at the generated files.

---

## Applying the Configuration

Once you have your certificate files, enter the paths in the TUI config editor:

```
./config  →  1 — System Configuration  →  Server Setup
```

Scroll to the V3Net hub fields and set **Hub TLS Cert** and **Hub TLS Key**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                           Server Setup                               │
│                                                                      │
│  V3Net Hub       : Y                                                 │
│  Hub Host        :                                                   │
│  Hub Port        : 8765                                              │
│  Hub TLS Cert    : /etc/letsencrypt/live/hub.example.com/fullcha...  │
│  Hub TLS Key     : /etc/letsencrypt/live/hub.example.com/privkey...  │
│  Hub Data Dir    : data/v3net_hub                                    │
│  Auto Approve    : N                                                 │
│                                                                      │
│                          Screen 2 of 8                               │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  PgUp/PgDn - Screens  |  ESC - Return
```

Press **S** to save, then restart the BBS to activate the change. On startup the hub logs:

```
v3net hub starting addr=:8765
```

The log line does not explicitly indicate TLS status. If the hub starts
without errors and both `tlsCert` and `tlsKey` are set, TLS is active. A
quick way to verify is to connect with `curl`:

```bash
curl -I https://hub.example.com:8765/
```

Leaf nodes connecting to your hub should use `https://` in their `hubUrl`.

---

## File Permissions

The BBS process must be able to read the certificate and key files. If
using Let's Encrypt on Linux, the private key is readable only by root by
default. Options:

- Run the BBS as root (not recommended).
- Add the BBS user to the `ssl-cert` group and adjust permissions:
  ```bash
  sudo chgrp -R ssl-cert /etc/letsencrypt/live /etc/letsencrypt/archive
  sudo chmod g+rx /etc/letsencrypt/live /etc/letsencrypt/archive
  ```
- Copy the certificate files to a directory the BBS user owns (update paths
  in the renewal hook).

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Hub fails to start with "open cert: no such file" | Certificate path is wrong | Verify the `tlsCert` path exists and is readable |
| Hub fails to start with "open key: no such file" | Key path is wrong | Verify the `tlsKey` path exists and is readable |
| Hub fails with "tls: private key does not match public key" | Mismatched cert/key pair | Re-generate or re-download both files together |
| Leaf nodes get "certificate signed by unknown authority" | Self-signed cert not trusted | Use Let's Encrypt, or configure leaf to skip verification (if supported) |
| Cert expired after 90 days | Let's Encrypt renewal not running | Check `certbot renew` timer/cron and restart hook |
