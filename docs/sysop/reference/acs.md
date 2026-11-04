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
| Door categories | `acs` in `doorCategories` (config.json) |
| Menu art | `{{acs}}...{{/}}` [conditional regions](menus/menu-system.md#conditional-regions-acs) |
| QWK | `acs_write` gates each reply in an uploaded REP packet |

Doors themselves are gated by `min_access_level`, not an ACS string.

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
`S20`, `FA`, `Y08:00/17:00`. Letters are case-insensitive (`s20` is
`S20`). Numeric comparisons are "at least": `S20` passes at level 20 and
above. There is no "less than" or "equal to" form.

| Term | Passes when | Argument | Notes |
|---|---|---|---|
| `S`*n* | access level ≥ *n* | number | `S` with no number fails |
| `D`*n* | access level ≥ *n* | number | the same test as `S`: there is no separate download level |
| `SYSOP` | access level ≥ `sysOpLevel` | — | `sysOpLevel` in config.json, default 255 |
| `COSYSOP` | access level ≥ `coSysOpLevel` | — | `coSysOpLevel` in config.json, default 250 |
| `F`*x* | the user has flag *x* | one letter or digit | case-insensitive; `FAB` (two letters) fails — use `FA&FB` |
| `I` | the user has flag `I` | ignored | the same test as `FI`; `I5` is `I` |
| `V` | the user is validated | ignored | `V5` is `V` |
| `U`*n* | the user's ID is *n* | number | |
| `P`*n* | file points ≥ *n* | number | |
| `E`*n* | uploads ÷ calls × 100 ≥ *n* | number | see [Ratios](#ratios). Uploads per call, not posts per call |
| `B`*n* | uploads ÷ downloads × 100 ≥ *n* | number | see [Ratios](#ratios) |
| `T`*n* | at least *n* minutes left this call | number | passes for anyone with no time limit |
| `Z`*text* | *text* appears in the user's private note | letters, digits, `/`, `:` | case-insensitive; `Z` with no text passes for everyone |
| `C`*n* | the current message conference's ID is *n* | number | |
| `X`*n* | the current file conference's ID is *n* | number | |
| `H`*n* | the hour now is *n* | 0–23 | see [Clock terms](#clock-terms) |
| `W`*n* | today is weekday *n* | 0 (Sunday) – 6 (Saturday) | see [Clock terms](#clock-terms) |
| `Y`*hh:mm*/*hh:mm* | the time now is in the range | two times, 24-hour | both minutes included; see [Clock terms](#clock-terms) |
| `L` | the caller is local | ignored | needs a session; see below |
| `A` | the caller's connection has a terminal | ignored | needs a session; see below |

A term whose argument isn't valid (`Sx`, `H25`, `Y8/17`) fails, as does an
unknown letter (`Q`).

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
  `S<20` becomes `S` and `20` with no operator between them, so it fails for
  every level. `F(A)` becomes `F` and `A`, and fails even for a user with
  flag A — write `FA`.
- **Two `!` in a row are an error**: `!!FA` fails. Use `FA`.
- **A string that can't be read as a whole fails**: a trailing operator
  (`S20&`), an unclosed parenthesis (`(S20`), or only spaces.

### Failing terms and `!`

A term that fails because it is malformed (an unknown letter, a bad
argument, a session term with no session) is false like any other failing
term, and `!` turns false into true. `!Q`, `!Sx` and, without a session,
`!L` all pass. Write negated terms carefully.

A string that fails as a whole (the cases in [Syntax](#syntax) above)
fails outright: `!` can't rescue it.

Errors and bad terms are logged (at `ERROR` and `WARN`) when a caller hits
them, not when the board starts.

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
