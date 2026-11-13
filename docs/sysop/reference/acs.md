# Access Control Strings (ACS)

An Access Control String decides who may use something on the board: a menu,
a menu command, a message or file area, a conference. It is a short
expression such as `s20&fC` (level 20 or higher and flag C). This page
describes every term and operator as ViSiON/3 evaluates them today,
including the places where a string doesn't do what it seems to say.

The examples on this page are checked by `internal/menu/acs_reference_test.go`.

## Where ACS is used

| Where | Field |
|---|---|
| Menus | `ACS` in the menu's `.MNU` file: who may enter the menu |
| Menu commands | `ACS` on each command in the menu's `.CFG` file |
| Message areas | `acs_read`, `acs_write` in `message_areas.json` |
| File areas | `acs_list`, `acs_upload`, `acs_download` in `file_areas.json` |
| Conferences | `acs` in `conferences.json` |
| Doors | `acs` in `doors.json`, together with `min_access_level` |
| Door categories | `acs` in `doorCategories` (config.json) |
| Menu art | `{{acs}}...{{/}}` [conditional regions](menus/menu-system.md#conditional-regions-acs) |
| QWK | `acs_write` gates each reply in an uploaded REP packet |

One of these checks runs without a caller's session: a message area's
`acs_write` for replies uploaded through the QWK API. See
[Terms that need a session](#terms-that-need-a-session) for what that changes.
A REP packet uploaded during a call, the default areas chosen at login and
conditional regions in menu art are all checked with the caller's session.

## Empty and `*`

An empty string and a lone `*` pass that check for everyone, including a
caller who hasn't logged in yet, which is why the login menu's commands use
`*`. They only pass the check they are on: an area with an empty `acs_read`
still sits in a conference with its own `acs`.

`*` works only on its own. `**` or `*&S10` is not a wildcard (see
[Syntax](#syntax)).

Before login there is no user, and every other string fails, including a
negated one such as `!FA`.

## Terms

A term is a letter, usually followed by an argument with no space:
`S20`, `FA`, `Y08:00/17:00`. Most letters also have a readable name for the
same test: `LEVEL>=20` is `S20`, `FLAG:A` is `FA`. Letters and names are
case-insensitive (`s20` is `S20`, `level>=20` is `LEVEL>=20`).

A number after a letter has the letter's own meaning: "at least" for `S`,
`D`, `P`, `E`, `B` and `T` (`S20` passes at level 20 and above), "exactly" for
`U`, `C`, `X`, `H` and `W` (`H8` is 8 am). Every numeric term also takes a
comparison or a range: see [Comparisons and ranges](#comparisons-and-ranges).

| Term | Name | Passes when | Argument | Notes |
|---|---|---|---|---|
| `S`*n* | `LEVEL` | access level ≥ *n* | number | `S` with no number fails |
| `D`*n* | no name | access level ≥ *n* | number | the same test as `S`: there is no separate download level |
| `SYSOP` | — | access level ≥ `sysOpLevel` | — | `sysOpLevel` in config.json, default 255 |
| `COSYSOP` | — | access level ≥ `coSysOpLevel` | — | `coSysOpLevel` in config.json, default 250 |
| `F`*x* | `FLAG:`*x* | the user has flag *x* | one letter or digit | case-insensitive; `FAB` (two letters) fails — use `FA&FB` |
| `I` | no name | the user has flag `I` | ignored | the same test as `FI`; `I5` is `I` |
| `V` | `VALIDATED` | the user is validated | ignored | `V5` is `V` |
| `U`*n* | `USER` | the user's ID is *n* | number | |
| `P`*n* | `FILEPOINTS` | file points ≥ *n* | number | |
| `E`*n* | `UPLOADSPERCALL` | uploads ÷ calls × 100 ≥ *n* | number | see [Ratios](#ratios). Uploads per call, not posts per call |
| `B`*n* | `ULDLRATIO` | uploads ÷ downloads × 100 ≥ *n* | number | see [Ratios](#ratios) |
| `T`*n* | `TIMELEFT` | at least *n* minutes left this call | number | passes for anyone with no time limit |
| `Z`*text* | `NOTE:`*text* | *text* appears in the user's private note | letters, digits, `/`, `:`, `-` | case-insensitive; `Z` with no text passes for everyone, `NOTE:` with none is a bad term |
| `C`*n* | `MSGCONF` | the current message conference's ID is *n* | number | |
| `X`*n* | `FILECONF` | the current file conference's ID is *n* | number | |
| `H`*n* | `HOUR` | the hour now is *n* | 0–23 | see [Clock terms](#clock-terms) |
| `W`*n* | `WEEKDAY` | today is weekday *n* | 0 (Sunday) – 6 (Saturday) | see [Clock terms](#clock-terms) |
| `Y`*hh:mm*/*hh:mm* | `TIME:`*hh:mm*/*hh:mm* | the time now is in the range | two times, 24-hour | both minutes included; see [Clock terms](#clock-terms) |
| `L` | `LOCAL` | the caller is local | ignored | needs a session; see below |
| `A` | `TERMINAL` | the caller's connection has a terminal | ignored | needs a session; see below |

A term whose argument isn't valid (`Sx`, `H25`, `Y8/17`) fails, as does an
unknown letter (`Q`).

### Comparisons and ranges

The numeric terms (`S`, `D`, `U`, `P`, `E`, `B`, `T`, `C`, `X`, `H`, `W`)
take a comparison before the number, or a range:

| Form | Passes when the value is | Example |
|---|---|---|
| *n* | the letter's own meaning (see above) | `S20`, `H8` |
| `=`*n* | exactly *n* | `S=20`, `HOUR=8` |
| `<`*n*, `<=`*n* | below *n*, at most *n* | `S<20`, `LEVEL<=19` |
| `>`*n*, `>=`*n* | above *n*, at least *n* | `S>19`, `LEVEL>=20` |
| *a*`-`*b*, `=`*a*`-`*b* | from *a* to *b*, both included | `S20-99`, `LEVEL=20-99`, `H8-17` |

There is no "not equal": write `!(S=20)`. A comparison is part of the term,
with no spaces in it: `S < 20` is not `S<20`.

A range runs from low to high. Only the clock terms wrap around: `H22-6` is
10 pm through 6:59 am, and `W5-1` is Friday through Monday (`W1-5` is Monday
through Friday). Any other backwards range, such as `LEVEL=99-20`, is an
error.

`B` with no downloads yet and `T` for a user with no time limit have no
number to compare: they count as above every number, so `B>=50` and `T>30`
pass and `B<50`, `T<30` and any range fail. `E` with no calls yet fails
whatever the comparison.

### Readable names

A name stands for its letter, with the same test and the same argument:

- The numeric names always take a comparison: `LEVEL>=20`, `HOUR=8-17`,
  `USER=5`. A name with a bare number (`LEVEL20`) or nothing after it is read
  as its first letter with an argument, as it always has been, so `LEVEL` on
  its own is `L`, not a level test.
- `FLAG`, `NOTE` and `TIME` take a colon: `FLAG:A`, `NOTE:trusted`,
  `TIME:22:00/06:00`. `FLAG:` takes one flag letter or digit, like `F`.
- `VALIDATED`, `LOCAL` and `TERMINAL` take nothing.

`D` and `I` have no names: `D` is the same test as `S`, and `I` the same as
`FI`.

### Terms from the user's record

These are names only, with no letter:

| Term | Passes when | Notes |
|---|---|---|
| `CALLS`*op n* | the user's number of calls compares | `CALLS>=10`, `CALLS=1-5` |
| `POSTS`*op n* | messages posted compares | |
| `UPLOADS`*op n* | files uploaded compares | |
| `DOWNLOADS`*op n* | files downloaded compares | |
| `AGE`*op n* | the account's age in days compares | see below |
| `REALNAME` | the user has a real name set | takes nothing |

They take a comparison or a range like the numeric names
([Comparisons and ranges](#comparisons-and-ranges)); their ranges don't wrap.

`AGE` is whole calendar days since the user signed up, counted in the
board's `timezone` setting (or `VISION3_TIMEZONE`, then `TZ`): 0 on the day
of sign-up, 1 from the next midnight. A user with no sign-up date on record
has no age, so for them a string with `AGE` in it fails as a whole, whatever
the comparison and whatever its other parts say (see
[Problems and missing context](#problems-and-missing-context)). `vision3
doctor` lists such users under **user sign-up dates**.

### Admin roles and permissions

These are names only, taking a colon, for the
[admin roles](users/admin-roles.md) in `configs/roles.json`:

| Term | Passes when | Notes |
|---|---|---|
| `PERM:`*permission* | the user holds the permission: a SysOp (level ≥ `sysOpLevel`), or a user with a role that grants it | `PERM:users.validate`. Use this to offer an admin command: the command checks the same thing. `PERM:messages.moderate` answers for the caller's current message area, where a co-SysOp and the area's sponsor hold it too, and `PERM:files.manage` for their current file area, where a co-SysOp holds it too |
| `ROLE:`*name* | the user's record lists the role | `ROLE:validator`. Being a SysOp doesn't make anyone a member |
| `GROUP:`*name* | the user's record lists the group | `GROUP:fsxnet-ops`. A [group](users/admin-roles.md#groups) only identifies people; it grants nothing |

A permission the BBS doesn't define (`PERM:users.vaildate`) is an error, like
an unknown letter. A role name follows the rules in `roles.json`: a letter,
then letters, digits, `_` and `-`, and so do group names. A `ROLE:` or
`GROUP:` term naming a role or group `roles.json` doesn't define fails the
whole string (see
[Problems and missing context](#problems-and-missing-context)), so a
misspelt `!ROLE:validatr` can't pass for everyone.

### Ratios

`E` and `B` work out a percentage and round it down before comparing:

- `E30` with 3 uploads in 10 calls: 30%, passes. `E31` fails.
- `E33` with 1 upload in 3 calls: 33.3% rounds down to 33, passes. `E34` fails.
- `E` with no calls yet fails, even `E0`.
- `B50` with 3 uploads and 6 downloads: 50%, passes.
- `B` with no downloads yet passes, whatever the number.

### Time left

`T`*n* passes when the caller has at least *n* minutes left on this call,
counting time credited back for chat. Users with no time limit (a time
limit of 0, or co-sysop level and up) always pass. Checked without a session
(see below), the call is taken to have just started, so a user with a
60-minute limit passes `T60`.

### Clock terms

`H`, `W` and `Y` read the server's local clock: the zone in the `TZ`
environment variable when ViSiON/3 started, or the system's. The board's
`timezone` setting (and `VISION3_TIMEZONE`) does not affect them.

Both ends of a `Y` range are whole minutes and both are included:
`Y08:00/17:00` runs from 8:00:00 to 17:00:59. A range whose end is before its
start runs past midnight: `Y22:00/06:00` is 10 pm through 6:00 am.
`Y00:00/23:59` is the whole day.

### Terms that need a session

`L` and `A` look at the caller's connection:

- `L` passes when the connection is a local pipe or Unix socket, or comes
  from a loopback address (`127.x.x.x` or `::1`).
- `A` passes for every telnet connection, and for an SSH connection that
  asked for a terminal (a PTY). It doesn't check the terminal's encoding or
  whether it shows ANSI.

Checked without a session (replies uploaded through the QWK API), a string
with `L` or `A` in it
fails as a whole, whatever its other parts say: `L`, `!L` and `SYSOP|L` all
fail there, for sysops too (see
[Problems and missing context](#problems-and-missing-context)).

## Operators

| Operator | Meaning |
|---|---|
| `&` | and |
| `\|` | or |
| `!` | not: the term or group after it |
| `(` `)` | grouping |

`!` is applied first, then `&`, then `|`, left to right:

- `S99|FA&FC` is `S99|(FA&FC)`.
- `!FA&S10` is `(!FA)&S10`.
- `!(FA|FB)` passes when the user has neither flag.

Spaces around operators are allowed: `S20 & FA`.

## Syntax

- **Terms need an operator between them.** `S20FA` is read as `S` with the
  argument `20FA`, which isn't a number. `S20 FA` (a space, no operator) is
  two terms with nothing between them.
- **Only letters, digits, `/`, `:`, `.`, `_`, operators, parentheses and
  spaces are allowed.** `.` and `_` are for permission and role names
  (`PERM:users.validate`, `ROLE:msg_ops`). The comparison characters `<`, `>`, `=` and `-` are allowed
  only inside a term, between term characters: `S<20` and `H8-17` are
  fine, `S20-` is not. Any other character is an error: `S@20`, `F(A)`
  (write `FA`) and a stray `-` all fail.
- **A comparison has to read cleanly**: `LEVEL=99-20`, `S<<20`, `S<20-30`
  (a range takes `=` or nothing) and `H25-3` are errors.
- **Every term has to be one the board knows, with a value it can use**:
  `Q`, `Sx`, `FAB`, `H25` and `Y8/17` are errors.
- **Two `!` in a row are an error**: `!!FA`. Use `FA`.
- **The string has to read as a whole**: a trailing operator (`S20&`), an
  unclosed parenthesis (`(S20`), or only spaces are errors.

### Problems and missing context

A string with any of the problems above fails for every caller, sysops
included, and `!` can't make it pass: `!Q`, `S10|!Q` and `S20-` fail for
everyone. The same rule holds wherever the string is used: menus, commands,
areas, conferences, doors, door categories, menu art and QWK.

A term that needs something the check doesn't have fails the whole string
in the same way, whatever the other parts say:

- `L` and `A` checked without a session (see
  [Terms that need a session](#terms-that-need-a-session));
- `AGE` for a user with no sign-up date;
- `ROLE:`*name* or `GROUP:`*name* for a role or group `configs/roles.json`
  doesn't define.

So `!L`, `SYSOP|L`, `!AGE>=7` and `!ROLE:validatr` fail where those values
are missing,
rather than letting `!` or another branch pass. With the session or the
date there, they are checked as usual. Other terms keep their documented
outcomes without a session: `T` counts the call as just started (see
[Time left](#time-left)).

Every problem is logged at `ERROR` once when the board starts, and once the
first time a caller's check reads a string the startup scan doesn't cover;
see [Checking your strings](#checking-your-strings).

### Strings that changed meaning

Up to v0.9.7, a term with an unknown letter or a bad argument was false, so
`!` made it pass, and a character the parser didn't know was dropped,
leaving the rest of the string working. A door's `acs` was the only place
held to the strict rule. Every string `vision3 doctor` reported as a
**WARN** before is now a **FAIL**, and denies every caller:

| String | Before | Now |
|---|---|---|
| `!Q` | everyone | no one |
| `S10\|!Q` | everyone | no one |
| `H25\|S50` | level 50 and up | no one |
| `S20-` | level 20 and up | no one |
| `S50@` | level 50 and up | no one |
| `!L`, without a session | everyone | no one |
| `SYSOP\|L`, without a session | sysops | no one |

Run `vision3 doctor` and fix what its **Access control** section lists.
A string that read cleanly before, with no session term checked outside a
call, means what it did.

## Checking your strings

`vision3 doctor` checks every ACS string the board declares, including menus
no caller has opened yet. Its **Access control** section names the file, the
record (menu, command, area, conference, door), the field and the column of
each problem:

```text
[FAIL]  ACS: menus/v3/cfg/MAIN.CFG  command 2 ("X"), ACS "S255&": column 5: '&' needs a term on each side
[FAIL]  ACS: configs/message_areas.json  area SYSOP, acs_write "!Q": column 2: "Q": Q is not an ACS term
```

- **FAIL** (doctor exits 2): the string has a problem, so it denies every
  caller until it is fixed (see
  [Problems and missing context](#problems-and-missing-context)).
- **WARN** (doctor exits 1): the string reads cleanly but uses `L` or `A`
  where it is checked without a session, a message area's `acs_write` for
  replies uploaded through the QWK API. Callers in a session are checked as
  usual, but every QWK API reply to that area is refused.

```text
[WARN]  ACS: configs/message_areas.json  area LOCALS, acs_write "S20|!L": uses L or A, which fail the whole string for replies uploaded through the QWK API, where there is no session
```

Doctor's **user sign-up dates** check lists users with no sign-up date: a
warning when one of the board's strings uses `AGE`, since that whole string
fails for them.

A string with a `ROLE:` or `GROUP:` term for a role or group
`configs/roles.json` doesn't define is a **FAIL**, since it denies every
caller:

```text
[FAIL]  ACS: menus/v3/cfg/MAIN.CFG  command 30 ("/VAL"), ACS "ROLE:validatr": names ROLE:validatr, which configs/roles.json doesn't define, so it fails for every caller
```

Doctor's **admin roles** check reports problems in `roles.json` itself; see
[Admin Roles](users/admin-roles.md#rolesjson).

The BBS runs the same check when it starts and after it reloads a changed
config file, and logs each problem once while it stays (failures at
`ERROR`, warnings at `WARN`), with the same text; a problem that is fixed and later comes back is logged again. It
never refuses to start over a bad string and never changes one: a bad string
denies every caller until it is fixed.

## Testing a string for a user

The ACS tester answers one question: does this expression pass for this
user? It is in both user editors, for sysops only: **ACS Test** on the field
editor in [`./ue`](users/user-editor.md#acs-test), and `T` in the
[Admin Menu's user editor](users/admin-menu.md#t--acs-test). Type an
expression and the tester shows how each part of it came out for the
selected user, and why.

The context is the tester's own, shown on screen with every result. It
starts as **no session** and **the current time**: the tester never uses your
own call as the selected user's. You can change both:

- **Session.** With none, a string with `L` or `A` in it fails and `T`
  counts the call as just started, as for the checks the board makes
  outside a call (see
  [Terms that need a session](#terms-that-need-a-session)). Or set a made-up
  call: local or not (for `L`), with a terminal or not (for `A`), how many
  minutes it has run and how much chat time was credited back (for `T`).
- **Time**, for `H`, `W`, `Y` and `T`: `hh:mm` for that time today, or
  `yyyy-mm-dd hh:mm`, in the server's clock.

For a level-20 user with flags A and C, no session, at 22:00:

```text
User:       Bob (#5), level 20
Session:    none: L or A fails the string, T as if the call just started
Time:       Wed 2026-11-04 22:00
Expression: S50&(FA|!FB)
            ---  ++  --
Result:     FAIL

  FAIL            S50&(FA|!FB)  both parts must pass: S50 failed
  FAIL              S50         level 20 is below 50
  PASS              FA|!FB      either part may pass: both passed
  PASS                FA        has flag A (flags: AC)
  PASS                !FB       FB failed, so this passes
  FAIL                  FB      doesn't have flag B (flags: AC)

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.
```

The line under the expression marks each term: `+` passed, `-` failed, `~`
needs a session the context doesn't have, a value the user's record
doesn't (`AGE` with no sign-up date), or a role or group `roles.json`
doesn't define.

A user with roles or groups has a **Roles:** or **Groups:** line under
**User:**. For `PERM:` the
tester says where the permission came from:

```text
  PASS              PERM:users.validate  has users.validate through role validator
```

A part that has such a term under it fails, and says so, whatever its other
part did. `S10|!L` with no session:

```text
Expression: S10|!L
            +++  ~
Result:     FAIL: a part needs a session the context doesn't have

  FAIL no session S10|!L  !L can't be checked here, so this fails whatever the
                          other part says
  PASS              S10   level 20 is at least 10
  FAIL no session   !L    L can't be checked here, so this fails too
  FAIL no session     L   needs a session and there is none, so the whole
                          string fails
```

A string with a problem (see [Syntax](#syntax)) shows its problems instead
of the list of parts, with `X` under each:

```text
Expression: S50&(FA|!Q)
                     X
Result:     FAIL: the string has a problem

the string has a problem, so it fails for everyone and '!' can't make it pass

  error at column 10: "Q": Q is not an ACS term
```

The tester and the board evaluate a string with the same code, so for the
same user, session and time they always agree. A pass is about this one
expression: a message area is also behind its conference's ACS, a door
behind its level and category, private mail behind its own rules.

## Examples

| String | Passes for |
|---|---|
| `s10` | level 10 and up |
| `fA` | users with flag A |
| `!fB` | users without flag B |
| `s20&fC` | level 20 and up with flag C |
| `s10\|fD` | level 10 and up, or anyone with flag D |
| `V&!FX` | validated users without flag X |
| `SYSOP\|U1` | sysops, and user 1 |
| `PERM:users.validate` | sysops, and users with a role granting `users.validate` |
| `(S50\|FM)&T10` | level 50 or flag M, with 10 minutes or more left |
| `S<20` | levels below 20 |
| `CALLS>=10&AGE>=7` | users with 10 calls or more who signed up a week or more ago |
| `REALNAME\|SYSOP` | users with a real name set, and sysops |
| `LEVEL=20-99&FLAG:C` | levels 20 to 99 with flag C |
| `!(S=20)` | every level but 20 |
| `S>=50\|FILEPOINTS>=40` | level 50 and up, or anyone with 40 file points |
