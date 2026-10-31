# V3Net Network Area List (NAL)

## What is the NAL?

The Network Area List (NAL) is the official list of message areas that a V3Net network carries. Think of it as the master directory of all the message boards available on a network like felonynet. It is what the area browser shows you, what the login question about new areas is drawn from, and what decides who may carry each area.

The NAL is **signed** by the network coordinator — the sysop who runs the hub. This signature ensures that nobody can tamper with the area list. Every node on the network can verify the signature before trusting the NAL.

## How Areas Work

Each message area has:

- **Tag** — A short identifier like `fel.general` or `fel.phreaking`
- **Name** — A human-readable display name
- **Description** — What the area is about
- **Access Mode** — Who can subscribe (see below)
- **Policy** — Content rules (max message size, ANSI allowed, etc.)

## Subscribing to Areas

Subscriptions can be managed from two places. In the config editor, open the
subscription's **Areas** (or **Browse Areas**) field under
**6 — ViSiON/3 Networking (V3Net) → 2. Subscriptions**. In the BBS, the V3Net
sysop menu (`V3NETM`, **3** on the Admin menu) has **Area Subscriptions**
(`V3NETAREAS`). You'll see a list of all areas the network offers:

```
[ V3Net: felonynet — Area Subscriptions ]

  TAG                 NAME              STATUS     LOCAL BOARD
  fel.general         General           ACTIVE     FelonyNet General
  fel.phreaking       Phreaking         PENDING    —
  fel.art             ANSI/ASCII Art    —          —

  [Space] subscribe/unsubscribe  [E]dit local board name
  [P]ropose new area             [Q]uit
```

- Press **Space** on an area to subscribe or unsubscribe
- Press **E** to set which local message base this area maps to
- Status shows `ACTIVE` (you're in), `PENDING` (waiting for approval), or blank (not subscribed)

A change made here applies at once; no restart is needed. When the hub adds
an area later, each SysOp is asked about it at login — see
[New Areas on Your Networks](v3net/message-areas.md#new-areas-on-your-networks).

## Access Modes

Each area has one of three access modes:

### Open
Any subscribed node can carry this area. Subscribe and you're immediately active. Most public discussion areas use this mode.

### Approval
The area manager must approve your subscription. After you subscribe, your status shows `PENDING` until the manager approves you from their sysop menu.

### Closed
Only nodes explicitly added by the area manager can access this area. You cannot subscribe on your own — the manager must add your node ID to the allow list.

A **deny list** is always enforced regardless of mode. If your node is on an area's deny list, you cannot subscribe no matter what.

## Proposing a New Area

Any sysop on the network can propose a new area. From Area Subscriptions, press **P** to open the proposal form:

```
[ V3Net: Propose New Area — felonynet ]

  Area Tag     : fel.________________
  Display Name : ________________________________
  Description  : ________________________________________________
  Language     : en
  Access Mode  : [Open] / Approval / Closed
  Allow ANSI   : [Y]

  [S]ubmit  [Q]uit
```

Area tags must follow the format `{prefix}.{name}` — for example, `fel.coding` or `fel.music`. The prefix is usually the network's short name.

After submitting, your proposal goes to the network coordinator for review. You'll be notified via the V3Net event stream when it's approved or rejected. On a hub with **Auto Approve Areas** set to `Y`, the area is added to the NAL as soon as the proposal arrives, with no review.

An area's name, description and language can't hold pipes or control characters.
The hub refuses a proposal that has them, and the screen shows its error.

## Text From Other Boards

Area names, descriptions, BBS names, proposal fields and error messages from a
hub come from other people's systems, so V3Net screens treat them as plain text.
This covers the login notices, the area browser, the registry and coordinator
screens, the config editor and the `v3net-handover` tool:

- A pipe shows as a pipe. `|CL` does not clear the screen, and `|12` does not
  change the colour.
- Control characters, including raw escape sequences, show as spaces.
- An area whose tag isn't a valid `prefix.name` is not offered, and it can't be
  added to your BBS. The config editor leaves it out of its area browser.

When you add an area, its local name is the hub's name with any pipes changed to
`/`. You can rename it and colour it yourself afterwards.

A V3Net area added before this check may hold a hub's text. When the BBS or the
config editor loads such a name, control characters become spaces and the pipes of
the screen codes (`|CL`, `|CR`, `|DE`, `|P`, `|PP`) become `/`. Colour codes stay,
so a name you coloured is unaffected, but a V3Net area's local name can't use
screen codes.

## For Area Managers

If you manage an area (the coordinator assigned you as manager), you'll see the **Area Access Requests** screen in the V3Net menu:

```
[ V3Net: Area Access Requests ]

  #  NETWORK     AREA TAG          BBS NAME              NODE ID   REQUESTED
  1  felonynet   fel.phreaking     The Underground BBS   A1B2C3D4  2d ago
  2  felonynet   fel.phreaking     Sector 7 BBS          E5F6A7B8  5h ago

  [A]pprove #  [D]eny #  [Q]uit
```

Type the action letter and the row number, for example `A 1` or `D2`.

- **Approve** — Grants the requesting node access to the area and activates its subscription
- **Deny** — Rejects the request, optionally with a reason, and adds the node to the area's deny list so it cannot re-request

The list covers every subscribed network and is refreshed after each action.

## For Network Coordinators

Coordinators see the **Coordinator Panel** in the V3Net menu:

```
[ V3Net: Coordinator Panel — felonynet ]

  [P]ending area proposals  (2)
  [M]anage area managers
  [Q]uit
```

The panel appears only when the NAL of a subscribed network names your node as its coordinator.

### Reviewing Proposals

When sysops propose new areas, you review them from the Pending Proposals screen. Type the action letter and the row number, for example `A 1` or `R2`.

- **Approve** — Asks for the access mode (`O`pen, `A`pproval or `C`losed; press Enter to keep the one proposed), then adds the area to the NAL and publishes it to all nodes. The proposing node becomes the area's manager.
- **Reject** — Declines the proposal, optionally with a reason, and notifies the proposing node

Proposals only wait here while the hub's **Auto Approve Areas** is `N`
(`hub.autoApproveAreas: false`). With it on, every proposal is added as it
arrives. A hub configured before the setting existed follows **Auto
Approve** instead, so an open hub that approves subscribers on its own also
approves areas until you set **Auto Approve Areas** to `N` explicitly. See
[V3Net Configuration](v3net/configuration.md#system-setup--server-setup).

### Changing an Area's Manager

**Manage area managers** lists every area in the NAL with its current manager. Type `C` and the row number, for example `C 3`, then enter the node ID of the new manager. The new manager must be an active subscriber of the network: the hub records the node's public key from its own registry, so you never paste a key. A sysop can find their node ID on the V3Net status screen.

The change is published to all nodes straight away, and the new manager sees the area's access requests from then on.

### Handing Over a Network

The hub signs the NAL with its own key, so the coordinator is always the hub operator. There is no separate coordinator transfer. To give a network to a new operator, move the hub:

1. Write down the hub's 24-word recovery phrase, or export it to a file (see [Key Recovery](v3net/recovery.md)).
2. Copy the hub's data directory to the new server.
3. On the new server, enter those 24 words in the recovery screen (`./config` → V3Net → Node Identity → `[R] Recover`), then start the hub.

The node ID stays the same, so the NAL, subscribers and area managers carry over unchanged. Point the hub's hostname at the new server; if the address changes instead, every subscriber has to update the hub URL in their V3Net config.

To share the work of running a network without handing it over, reassign individual areas to other sysops with **Manage area managers**.

Because the recovered hub has the same key, subscribers' pinned coordinator
key stays valid and nobody has to re-pin. If a network moves to a new key
(a new operator who wants their own identity, or retiring a key you no
longer trust), sign a handover with the old key first, as described next;
subscribers then move their pins by themselves. Without the old key (a hub
that could not recover its phrase) there is no handover, and every
subscriber's leaf rejects the new NAL until its sysop re-pins, as described
under [Accepting a New Key](#accepting-a-new-key-by-hand). Announce the new
key through a channel the subscribers already trust before you switch.

### Moving the Network to a New Key

A coordinator handover is a short statement, signed with the network's
current key, that names the new key. The hub keeps it and serves it with
the NAL, and each subscriber's leaf that checks it against its pinned key
moves the pin to the new key itself (see
[Signed Handovers](#signed-handovers)).

The recommended order is below: publish the handover, then switch the hub
to the new key. If the hub was switched first, the handover still works;
see [If the Hub Already Runs the New Key](#if-the-hub-already-runs-the-new-key).

1. Get the new key's public key: from the new hub's Node Identity screen
   (`./config` → V3Net → Node Identity), or its `v3net.key` file.
2. While the hub still runs with the old key, sign and publish the handover
   from the hub's machine, with the old `v3net.key`:

   ```bash
   go run ./cmd/v3net-handover \
     -keystore data/v3net.key \
     -hub      http://localhost:8765 \
     -network  felonynet \
     -new-key  <new public key>
   ```

   Use `-new-keystore <path>` instead of `-new-key` to read the key from
   the new `v3net.key`, and `-dry-run` to print the signed handover and
   check it without sending it. Run it once per network the hub carries. Before posting,
   the tool checks the key the hub's `/info` page serves, and refuses if it
   is neither the old key nor the new one (usually a wrong `-hub`,
   `-network` or keystore). It then checks the chain against the hub's
   record of who signed the NAL; see
   [Checking the Chain](#checking-the-chain).
3. Start the hub with the new key (swap in the new `v3net.key`, or recover
   the new identity's phrase). On start it re-signs each handed-over
   network's NAL with the new key, so subscribers move at their next fetch.

Do steps 2 and 3 close together. Between them the hub refuses every NAL
change it would sign with the old key (approving a proposal, changing an
area's access or manager), because signing with the old key would undo the
handover for subscribers that already follow the new key. Those changes
fail with `the network was handed over to another coordinator key` until
the hub runs with the new key; a proposal sent meanwhile waits in
**Pending area proposals** instead of being approved automatically.

The new key's node becomes the coordinator. When the hub signs the NAL
with the new key, the areas the old hub node managed move to the new node
in the same NAL update, and the hub logs `area manager moved to the hub
after a coordinator handover` for each one. Areas other sysops manage stay
theirs. To hand some of the moved areas back out, use **Manage area
managers**.

The hub only takes a handover signed by the key in force, and never one
back to a key the network already used. A NAL published to the hub must
always be signed by the key in force, which is the new key once the hub
holds a handover.

#### If the Hub Already Runs the New Key

If the hub was restarted with the new key before the handover was
published, subscribers see a hub key they have not pinned and pause syncing
with the hub. To recover, publish the handover now, exactly as in step 2:
sign it with the **old** `v3net.key` (keep a copy of it), and point `-hub`
at the running hub. The tool notes that the hub already runs the new key
and posts anyway. The hub accepts the handover and announces the change,
and the tool then fetches the NAL to confirm the hub serves it signed by
the new key. Subscribers move both pins and resume without re-pinning by
hand; a paused leaf re-checks the hub every five minutes, so allow that
long. No second restart is needed.

If the tool reports that the hub holds the handover but could not sign the
NAL with the new key, subscribers are still paused. Check the hub's log for
the cause (`could not re-sign the NAL after a handover`), fix it, and run
the same command again: the hub treats it as the same handover and retries
the signing.

Until the handover arrives, the hub may serve either NAL:

- **The old key's.** Nothing has changed the NAL since the restart. The
  hub re-signs it with the new key as soon as it accepts the handover.
- **The new key's.** Something changed the NAL first (merging
  `initialAreas` at startup, approving a proposal, changing an area's
  access or manager), and the hub signed it with the key it runs, as it
  always has when a key changes without a handover. Subscribers reject that
  NAL until the handover arrives. The hub remembers the key that signed the
  NAL before, and takes that key's handover to the new key all the same.

This is as safe as the usual order: the hub only takes a handover signed by
the key that signed its NAL before the switch, and while it runs a key no
handover names, only a handover to that key.

##### If the Key Changed More Than Once

If the hub's key changed more than once before any handover was published
(A, then B, then C, each restart re-signing the NAL), the hub remembers each
key that signed the NAL, up to the last eight. Bridge them all in one go:
pass the first key's keystore as `-keystore`, the keystores in between to
`-via` in the order the hub ran them (comma-separated), and the key the hub
runs now as `-new-key`:

```bash
go run ./cmd/v3net-handover \
  -keystore a.key \
  -via      b.key \
  -hub      http://localhost:8765 \
  -network  felonynet \
  -new-key  <C's public key>
```

The tool signs A to B with A's key and B to C with B's, and posts both as
one request. The hub takes the chain whole or not at all: every link must
be signed by the key before it, start at a key that signed the NAL, follow
the order those keys signed it, and end at the key the hub runs. Subscribers
on A or on B then follow it to C. Include every key that signed the NAL,
up to the one the hub serves it signed by now if that is not yet C. Only
keys that signed the NAL count: if the NAL is still signed by A (nothing
changed it while the hub ran B), post A to C directly, without `-via`.

Post the whole chain at once. Once the hub holds any handover it no longer
takes one from a remembered key, so posting B to C on its own would leave
subscribers still on A to [accept the new key by
hand](#accepting-a-new-key-by-hand). The same goes for a key you no longer
have: start the chain at the oldest key you still hold, add
`-omit-lost-keys`, and subscribers on older ones re-pin by hand.

##### Checking the Chain

You do not have to work out the chain yourself. Before posting, the tool
asks the hub which keys signed the NAL and prints the chain it needs: which
keystore to pass as `-keystore`, which to pass to `-via` and in what order,
and the `-new-key`. If yours does not match, the tool refuses and posts
nothing, so a wrong first try costs nothing. Run it with the keystore you
have, then again with the flags it prints.

Sometimes a key is really gone. A chain that leaves out a recorded key is
refused too, because it cannot be repaired afterwards: once the hub holds a
handover it takes none from remembered keys. If you no longer have that
key's keystore, add `-omit-lost-keys` to post the chain without it.
Subscribers still on that key [accept the new key by
hand](#accepting-a-new-key-by-hand). The flag does not let through a chain
the hub would refuse.

If the hub went back to a key it already ran (A, then B, then A), it cannot
bridge B itself, since it takes no handover back to a key the network used.
To keep subscribers on B, restart the hub with the new key first and post
the whole chain, B then A then the new key; the tool prints it. Otherwise
add `-omit-lost-keys`.

The tool believes the hub's answer only if it is signed by the hub key you
named, which is `-keystore`'s key or the new key, whichever `/info` says the
hub runs. An answer with a bad signature, or a successful one with none,
stops the tool: something other than that hub answered. Check `-hub` and any
proxy in front of the hub.

An older hub has no such record to give. The tool then prints `Could not
check the chain against the hub's signer history` and posts as before, and
the hub still checks the chain itself. It prints the same if the hub does
not know the sending node, and the post then fails with the `401` described
below.

`-dry-run` makes all of these checks and posts nothing, so you can ask the hub
which chain it needs before committing to one. It prints the signed handover,
then what a real run would do:

| Output | Meaning | Exit status |
|--------|---------|-------------|
| `Dry run: the checks pass and a real run would post this chain.` | The chain matches the hub's record | 0 |
| `Dry run: a real run would stop here:` and the reason | The hub runs neither key, its answer failed its signature, or the chain does not match; the reason names the chain to use where it can | 1 |
| `Could not check the chain:` | The hub could not be reached, so nothing was checked. You still get the signed handover, so this works offline | 0 |
| `Could not check the chain against the hub's signer history:` | The hub answered but has no record to give, as above. A real run would post | 0 |

Exit status 0 therefore does not by itself mean the chain was checked; read
the output.

**Checking the result.** The tool also checks the signature on the hub's reply
to the post, with a fresh nonce, against the same key. A reply that does not
verify, or an unsigned one from a hub that signed its reply about the signers,
proves nothing about what happened, because the post had already gone out. The
same goes for no reply at all (a timeout or a dropped connection). In each case
the tool does not report success or a refusal: it says the post may or may not
have taken effect, and exits with status 3. To find out, run the same command
with `-dry-run`. `the hub already holds this chain` means the post arrived; a
pass as a new chain means it did not. Posting again is also safe, since the hub
does not store a handover twice.

An older hub signs nothing. The tool says so and takes the status as sent, as
it always did. For a hub too old to report its signers, look for the handover
in `coordinator_handovers` in the network's NAL, or in the hub's log.

**Exit status.** A script can tell these outcomes apart by the tool's exit
status:

| Status | Meaning |
|--------|---------|
| 0 | The post worked, or a dry run found nothing that would stop a real run (see the table above) |
| 1 | The post failed or the hub refused it, a check stopped the tool before posting, or the options were missing or wrong |
| 2 | The flags could not be parsed: one the tool does not know, one with its value missing, or a bad value for `-dry-run` or `-omit-lost-keys`. Nothing was signed or posted |
| 3 | The outcome is unknown, as described above: the post may or may not have taken effect |

Treat 3 as neither success nor failure: run the command with `-dry-run` to find
out, or post again.

Status 1 does not always mean the handover had no effect. When the hub already
runs the new key, it may have stored the handover and then failed to sign the
NAL, or the tool may have been unable to confirm the NAL it then serves. The
tool says so; fix the cause and run the command again.

`go run` exits with status 1 for any non-zero status from the tool, so a script
that needs to tell these apart must build the tool and run the result:

```bash
go build -o v3net-handover ./cmd/v3net-handover
./v3net-handover -keystore data/v3net.key -hub http://localhost:8765 -network felonynet -new-key <new public key>
```

The tool sends the request as the old key's node, which the hub registered
as a subscriber while it ran with that key. Do not delete that node in
**Node Management** before the handover is published. If you already have,
the hub answers `401 unknown or inactive node`; send the request as the
hub's current node instead, which the hub always registers, by adding
`-auth-keystore data/v3net.key` (the new key). The handover itself is
still signed by the old key, which is what the hub checks.

## Coordinator Key Pinning

A NAL carries the key that signed it, so checking the signature alone only
proves that *someone* signed it. To know it was the coordinator, each leaf
pins the coordinator's public key for the network and rejects any NAL
signed by a different key, even one with a valid signature. It also checks
that the NAL's `coordinator_node_id` belongs to that key.

Where the pin comes from, in order of preference:

1. **You set it.** Paste the coordinator's key into **Coord Key** on the
   subscription (`./config` → V3Net → **2. Subscriptions**), or into
   `coordinatorPubKey` in `v3net.json`. Get the key from the network's
   operator over a channel you trust.
2. **The registry.** When you join from the registry browser and the
   registry entry publishes `coordinator_pubkey_b64` / `hub_pubkey_b64`,
   those keys are pinned when the subscription is saved. The join wizard's
   area list only shows a NAL signed by that coordinator key, or by a key
   the network moved to from it with [signed handovers](#signed-handovers);
   if the hub serves one signed by any other key, the wizard refuses to
   save. After a handover the registry key is still pinned, and the leaf
   moves the pin at its first fetch.
3. **First use.** Joining without a registry key pins the key that signed
   the NAL you picked areas from. With no key set at all (including every
   subscription made before pinning existed), the leaf pins the key of the
   first NAL it verifies, writes it to `coordinatorPubKey`, and logs
   `pinned key on first use`. If that write fails, the leaf keeps enforcing
   the key and retries the write at each fetch.

The editor's area browser checks every NAL it lists the same way: a valid
signature, a coordinator node ID that belongs to the key, and the
subscription's pinned key when one is set. Like the leaf, it also takes a
NAL signed by a key the pinned key's [signed handovers](#signed-handovers)
lead to, and then says `Network moved keys by signed handover`. It does
not change the pin itself; the running leaf moves it at its next fetch. A
NAL that fails is not listed; the browser shows the error instead.

The hub's own key, served by the network's `/info` page, is pinned the same
way into `hubPubKey` (**Hub Key**) and checked each time the leaf starts.
An updated hub also signs its replies and events with that key; a changed
hub key pauses syncing with the hub until you accept it (see
[Hub signatures](v3net/configuration.md#hub-signatures)).

### When the Key Changes

If the hub serves a NAL signed by a different key, the leaf:

- rejects it and keeps using the last NAL it accepted, so areas and
  subscriptions carry on as they were;
- logs an error, `key changed; rejected until the sysop accepts it`, with
  both the pinned and the offered key;
- records the offered key in `pendingCoordinatorPubKey` (shown as **Offered
  Coord** in the editor; `pendingHubPubKey` / **Offered Hub** for the hub
  key);
- queues a warning for every SysOp at their next login, and `./vision3
  doctor` reports it until it is dealt with.

Apart from a signed handover, described next, the pin is never updated on
its own. A changed key is either a planned change of coordinator or someone
impersonating the hub, and only you can tell which.

### Signed Handovers

When the coordinator moves the network to a new key with a signed handover
(see [Moving the Network to a New Key](#moving-the-network-to-a-new-key)),
the hub serves the handover with the NAL. A leaf offered a NAL signed by a
key other than its pin follows the handovers from the pinned key: each must
be for this network and signed by the key before it. If they lead to the
key that signed the NAL, the leaf:

- moves the pin to the new key and accepts the NAL;
- logs a warning, `key moved by a signed coordinator handover`, with the
  old and new key and the number of steps;
- writes the new key to `coordinatorPubKey` in `v3net.json`, but only if the
  entry still holds the old key, so a key you set yourself wins, and clears
  `pendingCoordinatorPubKey` if it held the new key;
- once the new key is saved, queues a notice for every SysOp at their next
  login saying the network moved its key by a signed handover (the
  `v3netKeyHandoverNotice` string). Several moves in a row give one notice.

A leaf that was offline through several handovers follows them all, up to
eight in a row. The hub key is checked the same way: when `/info` serves
the key that signs the NAL and the handovers lead to it from the pinned hub
key, `hubPubKey` moves too and syncing carries on without a pause.

Handovers that do not start at your pin, or that fail to verify, change
nothing: the NAL is rejected as above. A stolen old key could hand the
network to someone else this way, but a stolen key can already sign NALs,
so the handover adds no new risk.

### Accepting a New Key by Hand

Without a signed handover, changing coordinator is done by each subscriber
re-pinning by hand. This also works when a handover exists, and when a leaf
was offline through more handovers than it follows.

1. Confirm the new key with the network's operator through a channel you
   already trust (not the hub that is serving it).
2. Copy the offered key into **Coord Key** (or `coordinatorPubKey`), or the
   key the operator gave you if they differ. Saving clears the offered key.
   Do the same for **Hub Key** if the hub key changed too.
3. The running BBS picks up the edit to `v3net.json` by itself; no restart is
   needed, though it [waits for callers to log off](v3net/configuration.md#when-changes-to-v3netjson-apply). An edit that only changes keys is applied to the running
   subscription without restarting it, and the leaf re-fetches the NAL at
   once, so the new coordinator's area list takes effect straight away.

Blanking **Coord Key** also accepts the change, by pinning whatever the hub
serves next on first use. Only do that when you trust the hub you are
connected to.

## Troubleshooting

| Symptom | What is happening |
|---------|-------------------|
| An area the hub just added is not in the browser | The leaf re-fetches the NAL when the hub announces a change (`nal_updated`) and whenever its event stream reconnects. If the BBS was down, it catches up on the next start. |
| Subscribed, but status stays `PENDING` | The area is in approval mode and its manager has not acted yet. Approval-mode areas are the manager's call, not the coordinator's. |
| Cannot subscribe at all | The area is closed and your node is not on its allow list, or your node is on the deny list. Ask the area manager. |
| New areas stopped arriving; the log says `key changed; rejected until the sysop accepts it` | The hub is serving a NAL signed by a key other than the one pinned, with no handover leading to it from your pin. See [When the Key Changes](#when-the-key-changes). |
| Every subscriber paused on a changed hub key right after you moved the hub to a new key | The hub was switched before the handover was published. Publish it now with the old key; see [If the Hub Already Runs the New Key](#if-the-hub-already-runs-the-new-key). |
| `v3net-handover` fails with `409 handover is not signed by the coordinator key in force`, and the hub's key changed more than once without a handover | Post the whole chain from the first key in one request with `-via`; see [If the Key Changed More Than Once](#if-the-key-changed-more-than-once). |
| `v3net-handover` fails with `401 unknown or inactive node` | The old key's node is no longer registered with the hub (deleted in Node Management). Add `-auth-keystore` with the hub's current `v3net.key`; see [If the Hub Already Runs the New Key](#if-the-hub-already-runs-the-new-key). |
| The log says `key moved by a signed coordinator handover` | The coordinator moved the network to a new key, and your leaf followed. Nothing to do; see [Signed Handovers](#signed-handovers). |
| Coordinator Panel never appears | Your node is not the coordinator of any subscribed network. Only the hub's own node is; see Handing Over a Network. |

## Related Documentation

- [V3Net Message Areas](v3net/message-areas.md) — local areas, message flow, the login question
- [V3Net Configuration](v3net/configuration.md) — hub approval settings
- [V3Net Key Recovery](v3net/recovery.md) — moving a hub's identity

---

## Technical Reference

### NAL Document

The NAL is a signed JSON document. The coordinator's Ed25519 key signs a canonical representation of the NAL (all fields sorted alphabetically, signature field cleared). Any modification after signing causes verification to fail.

| Field | Meaning |
|-------|---------|
| `v3net_nal` | Format version |
| `network` | Network name |
| `coordinator_node_id`, `coordinator_pubkey_b64` | The hub's node ID and public key, which signed the document |
| `updated` | Date of the last change, `YYYY-MM-DD` UTC |
| `signature_b64` | Ed25519 signature over the canonical payload |
| `areas[]` | The areas, each with `tag`, `name`, `description`, `language`, `moderated`, `manager_node_id`, `manager_pubkey_b64`, `access`, `policy` and `added` (the date the hub added it, used by the login question) |
| `areas[].access` | `mode` (`open`, `approval` or `closed`), `allow_list` and `deny_list` of node IDs |
| `areas[].policy` | `max_body_bytes`, `allow_ansi`, `require_tearline` |
| `coordinator_handovers[]` | The coordinator handovers the hub holds for the network, oldest first, at most 32; left out when there are none. Added by the hub when it serves the NAL and not covered by the NAL's signature, since each carries its own. See [Coordinator Handovers](v3net/message-areas.md#coordinator-handovers) |

Area tags must match `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`.

### Where It Lives

- On the hub: in the hub data directory (`hub.dataDir`, normally
  `data/v3net_hub/`), seeded from `hub.initialAreas` on first start.
- On a leaf: fetched with `GET /v3net/v1/{network}/nal`, kept in memory, and
  re-fetched after a `nal_updated` event (with a short random delay so a
  hub's leaves do not all ask at once) and on every SSE reconnect. Each
  fetch must be signed by the pinned `coordinatorPubKey`, and its
  `coordinator_node_id` must be the first 8 bytes of SHA-256 of that key, in
  hex; otherwise it is rejected and the previous NAL stays in effect.
- `data/v3net_seen_areas.json` records which areas each network has offered
  this BBS, and `data/v3net_declined_areas.json` the areas each SysOp
  declined at login.

The optional git repository at `v3net-registry` serves as an archival mirror only. All area management happens natively through the V3Net protocol and ViSiON/3's sysop menus; the `v3net-bootstrap` tool can publish a NAL by hand when needed (see [V3Net Bootstrap Tool](v3net/bootstrap.md)).
