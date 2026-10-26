# V3Net Hub TLS Setup

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice.

This guide explains how to put a V3Net hub behind HTTPS so that traffic
between the hub and its leaf nodes is encrypted on the wire. The hub itself
always serves plain HTTP: Ed25519 signatures already authenticate every node
and every message, so TLS (Transport Layer Security) is a privacy layer you
add in front of the hub with a reverse proxy, not something the hub does
itself. You would want it when callers' chat or messages cross the open
internet and you would rather an eavesdropper on the path could not read them.

## Overview

The hub listens on `hub.host`:`hub.port` (default `8765`) and speaks HTTP
with JSON bodies plus a long-lived Server-Sent Events (SSE) stream per leaf.
A reverse proxy such as Caddy or nginx terminates HTTPS on port 443 and
forwards each request to the hub on the loopback interface. Leaves then use
an `https://` hub URL.

> **TLS covers the wire only.** Enabling HTTPS prevents eavesdropping on
> connections between leaf nodes and your hub. It does not encrypt messages
> stored in the hub database — hub operators have full read access to all
> messages in plaintext. See
> [Security Considerations](v3net/configuration.md#security-considerations) for
> the full picture.

> **Note:** Earlier V3Net builds had `tlsCert` and `tlsKey` keys in the hub
> block and **Hub TLS Cert**/**Hub TLS Key** fields in the config editor.
> They were removed; a hub serves plain HTTP and a reverse proxy handles
> certificates. If your `v3net.json` still has those keys, delete them.

You have two options for a certificate:

| Approach | Best For |
|----------|----------|
| **Let's Encrypt** (free, trusted) | Public hubs with a domain name |
| **Self-signed certificate** | Private networks, LAN-only hubs, or testing |

A self-signed certificate is not trusted by leaves' HTTP clients, and the
leaf has no option to skip certificate verification, so every subscriber
would need to install your certificate as trusted on their server. For
anything beyond a lab, use Let's Encrypt.

---

## Step 1: Bind the Hub to Loopback

```
./config  →  1 — System Setup  →  2. Server Setup
```

Set **Hub Host** to `127.0.0.1` (or `::1` for IPv6 loopback) and leave
**Hub Port** at `8765`, so only the proxy on the same machine can reach the
plain-HTTP listener. Press **S** to save and restart the BBS. The hub logs:

```
v3net hub starting addr=127.0.0.1:8765
```

---

## Step 2: Put a Reverse Proxy in Front

You need a **public domain name** pointing at the server (e.g.
`hub.example.com`); Let's Encrypt cannot issue certificates for bare IP
addresses or `.local` hostnames. Ports **80** and **443** must be reachable
from the internet.

### Option 1: Caddy (automatic certificates)

Caddy obtains and renews a Let's Encrypt certificate on its own. A complete
`Caddyfile`:

```
hub.example.com {
    reverse_proxy 127.0.0.1:8765 {
        flush_interval -1
    }
}
```

`flush_interval -1` passes SSE events through as they arrive instead of
buffering them. Start Caddy and you are done; there is no renewal hook to
write.

### Option 2: nginx with certbot

Install certbot and request a certificate:

```bash
# Debian / Ubuntu
sudo apt install certbot python3-certbot-nginx

# Fedora / RHEL
sudo dnf install certbot python3-certbot-nginx

sudo certbot certonly --nginx -d hub.example.com
```

Certbot writes the files to `/etc/letsencrypt/live/hub.example.com/`. Then a
server block that forwards to the hub:

```nginx
server {
    listen 443 ssl;
    server_name hub.example.com;

    ssl_certificate     /etc/letsencrypt/live/hub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hub.example.com/privkey.pem;

    location / {
        proxy_pass         http://127.0.0.1:8765;
        proxy_http_version 1.1;
        proxy_set_header   Host $host;
        proxy_set_header   X-Forwarded-For $remote_addr;

        # The /events stream stays open for hours: do not buffer or time it out.
        proxy_buffering    off;
        proxy_cache        off;
        proxy_read_timeout 1h;
    }
}
```

Certbot's systemd timer renews the certificate; nginx must reload to pick up
the new files. The `--nginx` plugin does that for you. If you used
`--standalone` instead, add a hook:

```bash
# /etc/letsencrypt/renewal-hooks/post/reload-nginx.sh
#!/bin/sh
systemctl reload nginx
```

```bash
sudo chmod +x /etc/letsencrypt/renewal-hooks/post/reload-nginx.sh
```

The BBS never needs restarting for a certificate change; only the proxy does.

### Windows

Use [win-acme](https://www.win-acme.com/), a free ACME client for Windows,
to obtain the certificate (`wacs.exe` as Administrator, **M** for manual
input, your domain, the self-hosting validation method, PEM files as the
store), then point the proxy's certificate settings at the files it writes.
Caddy runs on Windows as well and is the simpler choice there: the
`Caddyfile` above needs no change.

---

## Step 3: Point Leaves at HTTPS

Subscribers set their hub URL to `https://hub.example.com` (no port; the
proxy listens on 443). In the config editor that is the **Hub URL** field of
the subscription under **6 — ViSiON/3 Networking (V3Net) → 2. Subscriptions**.
If you list the network in the public registry, put the `https://` URL in its
`hub_url`.

Check from any machine:

```bash
curl -i https://hub.example.com/v3net/v1/networks
```

A JSON list of your networks over HTTPS means the proxy and the hub are
talking.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `502 Bad Gateway` from the proxy | The hub is not running, or listens on another address | Check `v3net hub starting addr=` in the BBS log against the proxy's upstream address |
| Leaves subscribe but never receive events | The proxy buffers the SSE stream | Turn buffering off for the hub location (`proxy_buffering off` / `flush_interval -1`) |
| Leaves drop and reconnect every minute | The proxy's read timeout closes idle streams | Raise `proxy_read_timeout`; the leaf's `leaf: SSE reconnecting` log line shows how often it happens |
| Leaf logs `certificate signed by unknown authority` | Self-signed certificate | Use Let's Encrypt, or install your CA certificate as trusted on each leaf |
| Certificate expired after 90 days | Renewal not running, or the proxy did not reload | Check the certbot timer and the post-renewal hook |
| `curl` works on `http://...:8765` but not on `https://` | Firewall blocks 443, or the DNS name points elsewhere | Open 443 and confirm the A/AAAA record |

---

## Technical Reference

| Setting | Where | Meaning |
|---------|-------|---------|
| `hub.host` | `configs/v3net.json`, **Hub Host** in Server Setup | Listen address. Blank binds every interface; `127.0.0.1` or `::1` keeps the plain-HTTP listener local to the proxy. IPv6 literals may be bare or bracketed. |
| `hub.port` | `configs/v3net.json`, **Hub Port** | Listen port, default `8765`. |
| `leaves[].hubUrl` | `configs/v3net.json`, **Hub URL** | What leaves connect to; `https://` with the proxy's public name. |

The hub has no TLS settings of its own and does not read `X-Forwarded-For`;
its node identification comes from the signed `X-V3Net-Node-ID` header, not
the client address, so a proxy in front of it changes nothing about
authentication. Every key is tabulated in the
[Manual Configuration Reference](v3net/manual-config.md).
