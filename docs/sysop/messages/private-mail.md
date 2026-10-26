# Private Mail

ViSiON/3 includes a dedicated user-to-user mail system. "Private" here means the message is addressed to a specific user rather than posted publicly to a board — it is **not** encrypted or secure in any modern sense. Messages are stored as plaintext in a JAM base on disk, where anyone with access to the files can read them. This is how BBS mail worked in the 90s.

Inside the BBS, a message with the `MSG_PRIVATE` JAM flag is shown only to its sender and its recipient, on every path that shows message content: the message reader and list, newscan, QWK packets and the script API. The mailbox commands are narrower still and show only mail addressed to you. The one exception is undeliverable mail, which a sysop can read; see [Read Filter](#read-filter).

## Setup

The private mail area is an ordinary local area with the tag `PRIVMAIL`. The
shipped `message_areas.json` already has one. To check or add it, open the
[Configuration Editor](configuration/configuration.md#configuration-editor-tui)
(`./config` → **3 - Areas and Conferences** → **Message Areas**) and make sure
an area with **Tag** `PRIVMAIL` and **Area Type** `local` exists. Leave
**ACS Read** and **ACS Write** blank so every caller can send and receive
mail; the read filter below, not the ACS, keeps mail private. The JSON form is
in the [Technical Reference](#technical-reference).

## User Access

Users access private mail through the Email Menu (press `E` from the main menu):

- **SENDPRIVMAIL** — Send private mail to another user; validates recipient exists, prompts for subject, launches the full-screen editor
- **READPRIVMAIL** — Read private mail; shows only messages addressed to the current user
- **LISTPRIVMAIL** — List private mail headers, for the same messages

Mail you have sent is not in your mailbox. It shows when you read the `PRIVMAIL` area with the message reader, list or newscan.

Posting with `COMPOSEMSG` while `PRIVMAIL` is the current area works the same way: the recipient must be an existing user, and the message is saved as private.

Private mail is always signed with the sender's handle, even on a board that
requires real names elsewhere. Netmail, which is private too, follows the
area's **Real Name Only** setting instead; see
[Real names and private mail](#real-names-and-private-mail).

An auto-signature is added to private mail as to any other post; a caller who
has **Ask each post** on is asked first (see [Auto-Signature](messages/autosig.md)).
A caller's [Twit List](messages/twit-list.md) hides private mail from the
authors on it as well.

## Troubleshooting

- **A caller cannot see mail that was sent to them.** The mail is addressed
  to a real name or to `Sysop` rather than a handle. Mail written, tossed or
  imported now is addressed by handle; for older mail run
  `v3mail readdress` (see [Readdressing private mail](messages/v3mail.md#readdressing-private-mail)).
- **The sysop cannot read someone's mail.** Expected. Only undeliverable mail
  (To is no account's handle) is readable at or above `sysOpLevel`.
- **A netmail the caller sent is not in their mailbox.** Mail you sent is
  never in the mailbox; read the netmail area itself. A netmail signed with
  a real name is still visible to its sender (see below).

---

## Technical Reference

### `message_areas.json`

Define the private mail area in `configs/message_areas.json`:

```json
{
  "id": 19,
  "tag": "PRIVMAIL",
  "name": "Private Mail",
  "description": "Private user-to-user mail",
  "acs_read": "",
  "acs_write": "",
  "allow_anonymous": false,
  "real_name_only": false,
  "conference_id": 1,
  "base_path": "msgbases/privmail",
  "area_type": "local"
}
```

The keys are the same as for any area; see
[Area Properties](messages/message-areas.md#area-properties).

### Read Filter

A private message is visible to a user when their **handle** matches its To
field, or the sender, ignoring case:

```go
func (m *DisplayMessage) VisibleTo(handle string) bool {
    if !m.IsPrivate {
        return true
    }
    // ...
    sender := m.From
    if strings.TrimSpace(m.SenderHandle) != "" {
        sender = m.SenderHandle
    }
    return strings.EqualFold(strings.TrimSpace(m.To), handle) ||
        strings.EqualFold(strings.TrimSpace(sender), handle)
}
```

This means:
- Only the handle counts. Real names are neither unique nor fixed, so a
  message addressed by real name is visible to no ordinary user. Mail is
  addressed by handle when it is written, tossed or imported;
  `v3mail readdress` fixes mail stored before that
  (see [Readdressing private mail](messages/v3mail.md#readdressing-private-mail)).
- `READPRIVMAIL` and `LISTPRIVMAIL` apply a narrower filter on top: private
  messages whose To is the current user's handle.
- There is no sysop bypass for delivered mail. A user at or above
  `sysOpLevel` can also read **undeliverable** mail, whose To is no account's
  handle, in the message reader, list and newscan. QWK packets and the script
  API make no such allowance.
- The rule applies on top of the area's read ACS, which alone does not protect
  private mail.
- None of this is encryption: anyone with filesystem access can read the base.

### Real names and private mail

Netmail is private, and a netmail area can require real names. A message
signed with the sender's real name would then be hidden from its own sender
by the handle rule above, so a local post whose From differs from the
poster's handle records the handle in a JAM subfield of ViSiON/3's own,
`SfldV3SenderHandle` (`0x5633`). The read filter treats that subfield as the
sender in place of From. It also stops a real-name From from matching an
unrelated account whose handle happens to equal it.

The subfield is written only for posts made from the BBS (compose and reader
replies), never for mail that arrives from a network, so imported mail cannot
claim to be from a local account. It is never exported: the tosser
builds packets from the standard fields only, and `v3mail pack` keeps it as
it is. Other JAM software skips subfields it does not know. Anonymous posts
record nothing.

### Message Attributes

Private messages combine two JAM attribute flags stored in the header's `Attribute` field:

- `MsgLocal` (0x00000001) — Created locally
- `MsgPrivate` (0x00000004) — Private message
