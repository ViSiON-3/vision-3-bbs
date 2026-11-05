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
| Doors | `acs` in `doors.json`, together with `min_access_level`; held to the [strict rule](#doors-the-strict-rule) |
| Door categories | `acs` in `doorCategories` (config.json) |
| Menu art | `{{acs}}...{{/}}` [conditional regions](menus/menu-system.md#conditional-regions-acs) |
| QWK | `acs_write` gates each reply in an uploaded REP packet |

Some of these checks run without a caller's session: the default message and
file area chosen at login, replies uploaded through the QWK API, and
conditional regions in menu art. See
[Terms that need a session](#terms-that-need-a-session) for what that changes.
A REP packet uploaded during a call is checked with the caller's session.

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

Checked without a session (default areas at login, replies uploaded through
the QWK API, conditional regions in menu art), both fail — so `!L` passes
there.

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

The parser is lenient, and some mistakes don't do what they look like:

- **Terms need an operator between them.** `S20FA` is read as `S` with the
  argument `20FA` and fails. `S20 FA` (a space, no operator) fails.
- **Characters other than letters, digits, `/`, `:`, operators, parentheses
  and spaces are dropped**, and a dropped character ends the term it is in.
  `S@20` becomes `S` and `20` with no operator between them, so it fails for
  every level. `F(A)` becomes `F` and `A`, and fails even for a user with
  flag A — write `FA`. The comparison characters `<`, `>`, `=` and `-` are
  part of a term only between term characters; elsewhere they are dropped
  too, so `S20-` is `S20`.
- **A comparison that can't be read fails the whole string**, like a trailing
  operator: `LEVEL=99-20`, `S<<20`, `S<20-30` (a range takes `=` or nothing)
  and `H25-3` all fail for everyone, and `!` can't make them pass.
- **Two `!` in a row are an error**: `!!FA` fails. Use `FA`.
- **A string that can't be read as a whole fails**: a trailing operator
  (`S20&`), an unclosed parenthesis (`(S20`), or only spaces.

### Failing terms and `!`

A term that fails because it is malformed (an unknown letter, a bad
argument, a session term with no session) is false like any other failing
term, and `!` turns false into true. `!Q`, `!Sx` and, without a session,
`!L` all pass. Write negated terms carefully. A door's `acs` is the
exception: see [Doors: the strict rule](#doors-the-strict-rule).

A string that fails as a whole (the cases in [Syntax](#syntax) above)
fails outright: `!` can't rescue it.

A string that read as a whole before comparisons were added keeps the
meaning it had: the comparison characters in it are still dropped. Only a
string that failed for everyone is read with comparisons. So `S<20` (which
used to fail as a whole) is now "level below 20", while `|S<20` (which the
stray `|` let read as `S|20`) still means what it did. `vision3 doctor`
reports the dropped characters in such a string.

Errors and bad terms are logged (at `ERROR` and `WARN`) when a caller hits
them, and once when the board starts; see
[Checking your strings](#checking-your-strings). An ignored character is
logged at `WARN` the first time a caller's check reads the string.

### Doors: the strict rule

A door's `acs` in `doors.json` must read cleanly. Anything `vision3 doctor`
reports for it, a failure or a warning, denies the door to every caller,
sysops included: `!Q`, `S20&`, `H25` and `S@20` all lock the door. A blank
or all-space `acs` is no rule. The config editor refuses such a string in a
door's **ACS** field, naming the column, and doctor reports it as a
**FAIL**. Every other ACS string keeps the lenient behaviour above.

## Checking your strings

`vision3 doctor` checks every ACS string the board declares, including menus
no caller has opened yet. Its **Access control** section names the file, the
record (menu, command, area, conference, door), the field and the column of
each problem:

```text
[FAIL]  ACS: menus/v3/cfg/MAIN.CFG  command 2 ("X"), ACS "S255&": column 5: '&' needs a term on each side
[WARN]  ACS: configs/message_areas.json  area SYSOP, acs_write "!Q": column 2: "Q": Q is not an ACS term; it is false, and the '!' before it makes that part pass for everyone
```

- **FAIL** (doctor exits 2): the string can't be read as a whole (see
  [Syntax](#syntax)), so it denies every caller; or it is a door's `acs`
  with any problem at all, which denies the door to every caller (see
  [Doors: the strict rule](#doors-the-strict-rule)).
- **WARN** (doctor exits 1): the string works, but not as it reads — a term
  that is always false (an unknown letter or a bad argument), or a character
  that is ignored. A future, stricter ACS check will change what these
  strings do, so fix them now.

The BBS runs the same check when it starts and after it reloads a changed
config file, and logs each problem once while it stays (failures at
`ERROR`, warnings at `WARN`) with the same text; a problem that is fixed and
later comes back is logged again. It never refuses to start over a bad
string and never changes one: a bad string keeps working, or failing, as it
always has.

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

- **Session.** With none, `L` and `A` fail and `T` counts the call as just
  started, as for the checks the board makes outside a call (see
  [Terms that need a session](#terms-that-need-a-session)). Or set a made-up
  call: local or not (for `L`), with a terminal or not (for `A`), how many
  minutes it has run and how much chat time was credited back (for `T`).
- **Time**, for `H`, `W`, `Y` and `T`: `hh:mm` for that time today, or
  `yyyy-mm-dd hh:mm`, in the server's clock.

For a level-20 user with flags A and C, no session, at 22:00:

```text
User:       Bob (#5), level 20
Session:    none: L and A fail, and T counts the call as just started
Time:       Wed 2026-11-04 22:00
Expression: S50&(FA|!Q)
            ---  ++  ?
Result:     FAIL

  FAIL            S50&(FA|!Q)  both parts must pass: S50 failed
  FAIL              S50        level 20 is below 50
  PASS              FA|!Q      either part may pass: both passed
  PASS                FA       has flag A (flags: AC)
  PASS invalid        !Q       Q failed, so this passes; that rests on an
                               invalid term, and a stricter ACS check will deny
                               it
  FAIL invalid          Q      "Q": Q is not an ACS term, so it is always false

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.
```

The line under the expression marks each term: `+` passed, `-` failed, `?`
invalid (an unknown letter or a bad argument), `~` needs a session the
context doesn't have. `^` marks a character that was dropped, and `X` the
place where a string that can't be read as a whole goes wrong; such a string
shows its problems instead of the list of parts.

Each part says whether it passed and, when the result isn't an ordinary
pass or fail, whether it rests on an invalid term or a missing session. A
part rests on one only when every part that decided it does: `S10|Q` passes on
`S10` alone, but `S10&!Q` passes only because `!` turns the invalid `Q`
into a pass. The tester shows what the board does today, so `!Q` passes in it
too; a future, stricter ACS check will deny it.

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
| `(S50\|FM)&T10` | level 50 or flag M, with 10 minutes or more left |
| `S<20` | levels below 20 |
| `LEVEL=20-99&FLAG:C` | levels 20 to 99 with flag C |
| `!(S=20)` | every level but 20 |
| `S>=50\|FILEPOINTS>=40` | level 50 and up, or anyone with 40 file points |
