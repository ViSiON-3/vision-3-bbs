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
- **DELETEPRIVMAIL** — Delete a mail (`D`): your current mail, the last of yours you read this call, after a Y/N; or, before you've read one, the mail whose number you give (the number `LISTPRIVMAIL` shows; `?` lists yours)
- **SENTPRIVMAIL** — View the mail you sent (`V`, ViSiON/2's View Outgoing Mail) in the message reader; `L` lists it. Reading it there doesn't mark your own unread mail as read.
- **COPYPRIVMAIL** — Copy a mail to another user (`C`): your current mail, or the one whose number you give, as `DELETEPRIVMAIL` picks it
- **FORWARDPRIVMAIL** — Forward a mail (`W`, hidden: the stock menu art doesn't list it): a copy, after which your own is deleted

Mail you have sent is not in your mailbox. `V` shows it, and so does reading the `PRIVMAIL` area with the message reader, list or newscan.

### Copying and forwarding

`C` and `W` ask for the user to send it to (a handle; a deleted account is
refused), then `Add a note of your own?`. With a note, the full-screen editor
opens for it; leaving the editor without saving sends nothing. The mail sent
is private, from you, titled `Fwd: <subject>` (once, however often it is
forwarded on), and reads:

```text
<your note, if any>

--- Forwarded mail ---
From: <original sender>
To: <original recipient>
Date: <date, in the board's time zone>
Subject: <original subject>

<original text>
```

The original's text goes as it was; you can't change it on the way. After a
forward your own copy is deleted, checked again as it is deleted. If it was
changed or deleted elsewhere meanwhile, the forward has still gone and you
are told yours was kept.

In the reader, `D` deletes a private message addressed to you, after a
`Delete this message? [Y/N]` check. In the mailbox it is on the bar as Del,
lit on your mail. `D` on the Email Menu (`DELETEPRIVMAIL`) does the same
from outside the reader. A deleted message is gone for good: the base is
packed at once, as when a co-sysop deletes.

You can unsend mail you sent while its recipient hasn't read it: `D` on it in
`V`'s reader (ViSiON/2's Kill Outgoing Mail) deletes it after the same check.
It counts as read once the recipient's last-read pointer in the mailbox has
reached it, and then it is theirs: Del is greyed and `D` does nothing. If they
read it while you're answering the Y/N, nothing is deleted and you're told.

Posting with `COMPOSEMSG` while `PRIVMAIL` is the current area works the same way: the recipient must be an existing user, and the message is saved as private.

Private mail is always signed with the sender's handle, even on a board that
requires real names elsewhere. Netmail, which is private too, follows the
area's **Real Name Only** setting instead; see
[Real names and private mail](#real-names-and-private-mail).

An auto-signature is added to private mail as to any other post; a caller who
has **Ask each post** on is asked first (see [Auto-Signature](messages/autosig.md)).
A caller's [Twit List](messages/twit-list.md) hides private mail from the
authors on it as well.

### Mail to a group

Address mail to `@name` to send it to every member of a
[user group](users/admin-roles.md#groups), for example `@fsxnet-ops`. That
works at **Send private mail to**, at **To:** when composing in `PRIVMAIL`,
and when copying or forwarding mail.

- Each member gets their own copy, which behaves like any other private
  mail: it has its own read status, and they can delete it. The sender gets
  no copy, and neither do deleted or banned members. A copy already
  delivered stays with its recipient, even after they leave the group.
- Every copy shows **To: @fsxnet-ops**. No one is shown who else is in the
  group, the sender included.
- Who may send to a group is the group's `mail_acs` in `roles.json`. If it
  has none, the group's members and co-SysOps and up may send. Anyone else
  is told `You may not send mail to @name.` before the editor opens.
- A group with no one to deliver to, or with more than 500 members, refuses
  the letter.
- **Sent mail** (`SENTPRIVMAIL`) lists the letter once, to `@fsxnet-ops`.
  `D` there unsends every copy no member has read yet. The copies already
  read stay, and the notice says how many.
- Replying to your copy asks `Reply to all of @fsxnet-ops?`. No (the
  default) replies to the sender alone; Yes sends the reply to the group,
  under the same rule for who may send. The sender of a group letter always
  replies to the group from their sent mail, since their copy names one
  member. Once the group is removed from (or renamed in) `roles.json`, that
  reply is refused with `@name is no longer a group.`, and a member's reply
  goes to the sender without asking.
- Who may send is checked again when the letter is saved, against the group
  as `roles.json` has it then, so a `mail_acs` tightened while someone is
  writing stops their letter.

A recipient that is exactly someone's handle is that user, so an old
account whose handle starts with `@` still gets its mail. New handles can't
start with `@`, and doctor warns about an old one that matches a group's
address.

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

A private message is visible to its recipient and its sender. The BBS
records the sender's **account ID** with each message it writes (sent,
replied, forwarded, group mail, QWK uploads, the script API), and the
recipient's when the address names an account; a QWK upload to a name no
account has records none for the recipient, and `helper post` records
neither (both sides of its mail are matched by handle). Where an ID
is recorded it decides: an account keeps its mail through a rename, and a
new account given an old handle can't read it. Where it isn't (mail from
before this, from other boards, or to an unresolved name), that side is
matched by **handle**,
ignoring case, against its To field or its sender (the handle a post signed
with a real name records, else From).

This means:
- Account IDs, then handles, are the only identities. Real names are neither
  unique nor fixed, so a message addressed by real name is visible to no
  ordinary user. Mail is addressed by handle when it is written, tossed or
  imported; `v3mail readdress` fixes mail stored before that
  (see [Readdressing private mail](messages/v3mail.md#readdressing-private-mail)).
- `READPRIVMAIL` and `LISTPRIVMAIL` apply a narrower filter on top: private
  messages for the current user (their ID, or their handle in To).
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
