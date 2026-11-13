# Admin Roles

An access level decides most of what a caller may do, and the admin
functions go with the top of it: the Admin Menu and the user editor are the
SysOp's (`sysOpLevel`, 255 by default). Giving someone a level high enough to
validate new users would give them every other admin function too.

A **role** hands out one admin job without raising anyone's level. You define
roles in `configs/roles.json`, each a name and the **permissions** it grants,
and give them to users in [`./ue`](users/user-editor.md) or the
[Admin Menu's user editor](users/admin-menu.md#r--roles-u--groups). The person keeps their
level, their time limit and their door privileges; the role adds that one
job and nothing else.

A board with no roles works exactly as before. SysOps keep every admin
function whether or not any roles are defined.

## Permissions

A permission is an admin action the BBS defines. There is one so far:

| Permission | What it allows |
|---|---|
| `users.validate` | Review accounts waiting for validation and approve them. Approving sets the account validated and raises its level to `regularUserLevel` if it is below that. |
| `messages.moderate` | Delete any message in a message area from the reader. A role can grant it in some areas or conferences only (see [Limiting a grant to some areas](#limiting-a-grant-to-some-areas)). It never covers private mail. |

`users.validate` is approval and nothing more. In the queue, someone who has
it below SysOp level sees each pending account's handle, real name,
group/location, sign-up date and number of calls, and can press `G` to
approve and `S` to save. They can't see the private note or the flags, and
can't change a level, flags, roles or password, ban, delete or
un-validate. The BBS checks this when the change is saved, not only by
hiding keys, so there is no way round it through another screen.

`messages.moderate` is what an area's [sponsor](users/sponsor-menus.md#moderating-messages)
can already do in their area, and a co-SysOp in every area. A role hands it
out without a sponsorship or a co-SysOp level. Editing an area's settings
stays with its sponsor and co-SysOps.

Every delete made through a role or a sponsorship is logged at `INFO` as
`message deleted by moderator`, with the area, the message, its author and
subject, and the grant that allowed it.

More permissions (file area management) are planned; each will be listed
here.

## roles.json

```json
{
    "roles": [
        {
            "name": "validator",
            "description": "Reviews new accounts and approves them",
            "permissions": ["users.validate"]
        }
    ]
}
```

| Field | Meaning |
|---|---|
| `name` | The role's name: a letter, then letters, digits, `_` and `-`, up to 24 characters, not ending in `-`. Case doesn't matter; it is stored in lower case. |
| `description` | Optional, for your own notes. |
| `permissions` | The permissions the role grants, from the table above. |

New installs ship this file with the `validator` role above, assigned to
nobody.

### Limiting a grant to some areas

A permission can be written as an object instead of a name. A scoped
permission (`messages.moderate`) then applies only in the areas it lists by
tag, and in every area of the conferences it lists by tag:

```json
{
    "name": "fsxnet_mod",
    "description": "Moderates fsxNet and the local general area",
    "permissions": [
        {"permission": "messages.moderate", "conferences": ["FSXNET"], "areas": ["GENERAL"]}
    ]
}
```

A plain name, or an object with neither list, covers every area. An object
that names `areas` or `conferences` but leaves the list empty is ignored,
rather than read as every area. Tags are
matched without regard to case. A tag that doesn't match any area or
conference covers nothing, and doctor warns about it. A permission that
isn't scoped (`users.validate`) can't be limited; a grant that tries is
ignored. Roles only ever add permissions. There are no deny rules and no
inheritance: a user with two roles has what both grant.

The BBS rereads `roles.json` when it changes, with no restart. Mistakes can
only ever grant less:

- A role with a bad or repeated name is ignored.
- A permission the BBS doesn't know (`users.vaildate`) grants nothing.
- A grant object with a key other than `permission`, `areas` and
  `conferences` (`"area"` for `"areas"`) is ignored, rather than read as a
  grant for every area.
- If the file isn't valid JSON, no role grants anything until it is fixed.
  Deleting the file removes every role.

Each problem is logged when the file loads, and `vision3 doctor` lists them
under **Access control**, with any user whose record names a role or group
the file doesn't define.

## Groups

A **group** is a named list of people, for ACS strings: `GROUP:fsxnet-ops`
on a menu command, a message area's `acs_read`, a door. Being in a group
grants no admin permission; it only identifies who is in it. Groups go in the
same file, under `groups`:

```json
{
    "roles": [],
    "groups": [
        {"name": "fsxnet-ops", "description": "Runs the fsxNet echoes"},
        {"name": "beta", "description": "Tries new doors first"}
    ]
}
```

Group names follow the same rules as role names. A group and a role may
share a name; they are separate. A group with a bad or repeated name is
ignored, and doctor reports it.

Put someone in a group in `./ue` (the **Groups** field) or the Admin Menu's
user editor (`U`), the same way as roles.

A group can also be sent mail as `@fsxnet-ops`: see
[Mail to a group](messages/private-mail.md#mail-to-a-group). `mail_acs`
decides who may send it; without one, the group's members and co-SysOps
and up may:

```json
{"name": "fsxnet-ops", "mail_acs": "S20&V"}
```

Doctor checks `mail_acs` like every other ACS string.

## Giving someone a role

- **`./ue`:** the **Roles** field on the edit screen. Type role names
  separated by commas or spaces; clear the field to remove them all.
- **Admin Menu:** `E` (Edit Users), select the user, press `R`.

Both check each name against `roles.json` as you type it, so a misspelt or
undefined role is refused instead of saved. Only a SysOp can assign roles.

## Taking a role away

Take the role off the user, or remove it or its permission from
`roles.json`. The change applies from the next protected action, even for
someone who is online with the validation queue already open: their next
save is refused. The BBS rereads the user's record just before each save,
picking up a `./ue` edit within a second.

## Menus: ROLE and PERM

Two [ACS terms](reference/acs.md#admin-roles-and-permissions) let menus
follow roles:

- `PERM:users.validate` passes for anyone who holds the permission: a
  SysOp, or a user with a role that grants it. Use this to offer a command,
  since it uses the same check as the command itself.
- `PERM:messages.moderate` answers for the caller's current message area:
  a co-SysOp, the area's sponsor, or a role that grants it there.
- `ROLE:validator` passes for a member of the role. A SysOp is not a member
  of a role unless it is assigned to them.
- `GROUP:fsxnet-ops` passes for a member of the group, in the same way.

The shipped Admin Menu stays SysOp-only. To give a validator a way into the
queue, add a command to a menu they can reach with
[`menuedit`](menus/menu-system.md), for example `/VAL` on the main menu:

```json
{
    "KEYS": "/VAL",
    "CMD": "RUN:VALIDATEUSER",
    "ACS": "PERM:users.validate",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Validating Users"
}
```

The login step [`NEWUSERVAL`](users/login-sequence.md#newuserval) also offers the
queue to anyone with `users.validate`, provided its `sec_level` lets them
reach it. The shipped `login.json` sets 255; lower it, or remove it, to
include your validators.

A string that names a role or group `roles.json` doesn't define fails for
every caller, SysOps included, and `!` can't make it pass. So a typo in
`!ROLE:validatr` or `!GROUP:fsxnet-op` locks everyone out rather than
letting everyone in. Doctor reports such strings as failures.

## The audit log

Every change made through the user editors is written to
`data/users/admin_activity.json` with who made it, to which account, and the
field. A change made through a role also records the grant that allowed it
in `notes`, for example `users.validate via role validator`.

## Co-SysOps and sponsors

Roles don't replace or widen what a level or a sponsorship already gives.

- A co-SysOp holds `messages.moderate` in every area by level, as before,
  and no other permission unless a role grants it: `users.validate` isn't
  theirs by level, any more than validation was before.
- A message-area [sponsor](users/sponsor-menus.md) keeps their area: they
  hold `messages.moderate` there, and only the sponsor and co-SysOps can
  change its settings. A role that grants moderation in an area doesn't
  make anyone its sponsor.
- Neither a role nor a sponsorship reaches `PRIVMAIL`: private mail has no
  moderator below co-SysOp level.
