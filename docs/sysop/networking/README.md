# Networking

Configure server access and inter-BBS message networking.

## Transport

- **[SSH Server](networking/ssh.md)** — pure-Go SSH server: setup, IPv6 hosts, host keys, legacy algorithms for retro clients
- **[Telnet Server](networking/telnet.md)** — Telnet server setup, terminal detection and protocol details
- **[WFC Sysop Console](how-to-guides/wfc-console.md)** — watch, chat with and manage callers remotely over the SSH server

## Message Networking

- **[V3Net Networking](v3net/message-areas.md)** — native inter-BBS message exchange over HTTP (HTTPS via a reverse proxy) using REST+SSE (Server-Sent Events) with Ed25519 authentication
- **[V3Net Configuration](v3net/configuration.md)** — field-by-field reference for `v3net.json`
- **[Joining FelonyNet](v3net/felonynet.md)** — step-by-step guide to joining the FelonyNet network
- **[FTN Echomail](messages/ftn-echomail.md)** — traditional FidoNet echomail via binkd and the built-in tosser
