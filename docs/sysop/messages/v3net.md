# V3Net Message Areas

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net areas are message areas that are networked using the V3Net protocol. Messages posted locally are relayed to the hub and distributed to all subscribed nodes. Messages posted on other nodes arrive via polling and SSE (Server-Sent Events).

## How V3Net Areas Differ from Other Types

| Feature | Local | FTN Echomail | V3Net |
|---------|-------|-------------|-------|
| Networking | None | FTN mailer + tosser | Built-in (REST+SSE) |
| Authentication | N/A | Packet passwords | Ed25519 signatures |
| Area discovery | Manual | AREAS.BBS | NAL (Network Area List) |
| Message dedup | N/A | MSGID CRC | UUID-based SQLite index |
| Transport | N/A | .PKT files via binkd | HTTPS (JSON) |
| Real-time delivery | No | Polling only | SSE push + polling |

## How Messages Flow

### Outbound (local user posts)

1. User writes a message in a V3Net area via the message editor
2. Message is saved to the local JAM base
3. A tearline and origin line are appended to the local copy
4. The `OnMessagePosted` hook sends the message to the hub via signed HTTP POST
5. The hub stores it and fans it out to all subscribed nodes via SSE events

### Inbound (remote message arrives)

1. The leaf client receives a message via polling or SSE notification
2. The dedup index is checked — if the UUID has been seen, the message is skipped
3. The message (with its remote tearline/origin) is written to the local JAM base
4. The UUID is recorded in the dedup index

## Creating V3Net Areas

V3Net areas are created automatically when your BBS subscribes to network areas via the NAL (Network Area List). You can also create them manually:

1. Add a message area in `configs/message_areas.json` or via the config editor
2. Set the area's `tag` to match the network area tag
3. Add a leaf subscription in `configs/v3net.json` with the `board` field matching the tag prefix

### Area Auto-Creation

When a leaf syncs the NAL from the hub, any network areas you are subscribed to that do not yet have a local message area are created automatically. The auto-created areas:

- Get the next available area ID and position
- Use the network area tag as the local tag
- Are placed in the default conference (ID 0)
- Have their `network` field set to the V3Net network name

## Message Format

V3Net messages in JAM bases look like standard echomail messages with a few additions:

- A `V3NETUUID` kludge line (hidden from users) stores the message's unique identifier for deduplication
- A tearline (`--- ViSiON/3 x.y.z/platform`) identifies the posting software
- An origin line (`* Origin: BBS Name (node_id)`) identifies the posting node

## Area Proposals

Users and sysops can propose new areas for a network. See the [NAL documentation](../../v3net-nal.md) for details on:

- Proposing new areas
- Access modes (open, approval, closed)
- Coordinator and area manager roles

## BBS Menu Integration

V3Net provides several menu screens accessible to sysops and users:

| Menu Command | Description |
|-------------|-------------|
| `V3NETSTATUS` | Shows V3Net connection status, node ID, and subscribed networks |
| `V3NETAREAS` | Browse and manage area subscriptions from the NAL |
| `V3NETPROPOSE` | Submit a new area proposal to the network coordinator |
| `V3NETREGISTRY` | Browse the public network registry to discover available networks |
| `V3NETCOORD` | Coordinator panel for approving/rejecting proposals (coordinator only) |
| `V3NETACCESS` | View and manage area access requests (coordinator only) |

These commands are configured in your menu `.CFG` files. The default V3Net menu is provided in `menus/v3/cfg/V3NETM.CFG`.

## Related Documentation

- [V3Net Configuration](../configuration/v3net-config.md) — field-by-field configuration reference
- [Joining FelonyNet](../../felonynet.md) — step-by-step guide
- [Network Area List (NAL)](../../v3net-nal.md) — area management and access control
- [Message Areas](message-areas.md) — general message area configuration
- [Header Placeholders](placeholders.md) — `@I@` (network name) and `@O@` (node ID) codes
