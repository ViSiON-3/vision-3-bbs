# Private Mail

ViSiON/3 includes a dedicated user-to-user mail system. "Private" here means the message is addressed to a specific user rather than posted publicly to a board — it is **not** encrypted or secure in any modern sense. Messages are stored as plaintext in a JAM base on disk, where anyone with access to the files can read them. This is how BBS mail worked in the 90s.

Inside the BBS, a message with the `MSG_PRIVATE` JAM flag is shown only to its sender and its recipient, on every path that shows message content: the mail reader, the message reader and list, newscan, QWK packets and the script API.

## Setup

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

## User Access

Users access private mail through the Email Menu (press `E` from the main menu):

- **SENDPRIVMAIL** — Send private mail to another user; validates recipient exists, prompts for subject, launches the full-screen editor
- **READPRIVMAIL** — Read private mail; shows only messages addressed to the current user
- **LISTPRIVMAIL** — List private mail headers

Posting with `COMPOSEMSG` while `PRIVMAIL` is the current area works the same way: the recipient must be an existing user, and the message is saved as private.

---

## Technical Reference

### Read Filter

A private message is visible to a user when their **handle** matches its To or
From field, ignoring case:

```go
func (m *DisplayMessage) VisibleTo(handle string) bool {
    if !m.IsPrivate {
        return true
    }
    // ...
    return strings.EqualFold(strings.TrimSpace(m.To), handle) ||
        strings.EqualFold(strings.TrimSpace(m.From), handle)
}
```

This means:
- Only the handle counts. Real names are neither unique nor fixed, so a
  message addressed by real name is visible to no ordinary user. Mail is
  addressed by handle when it is written, tossed or imported;
  `v3mail readdress` fixes mail stored before that
  (see [Readdressing private mail](messages/v3mail.md#readdressing-private-mail)).
- There is no sysop bypass for delivered mail. A user at or above
  `sysOpLevel` can also read **undeliverable** mail, whose To is no account's
  handle, in the message reader, list and newscan.
- The rule applies on top of the area's read ACS, which alone does not protect
  private mail.
- None of this is encryption: anyone with filesystem access can read the base.

### Message Attributes

Private messages combine two JAM attribute flags stored in the header's `Attribute` field:

- `MsgLocal` (0x00000001) — Created locally
- `MsgPrivate` (0x00000004) — Private message
