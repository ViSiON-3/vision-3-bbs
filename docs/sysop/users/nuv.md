# New User Voting (NUV)

New User Voting is ViSiON/3's implementation of ViSiON/2's NUV system. When a new user registers, their handle is placed in a pending queue. Existing users who meet a configurable minimum access level can then cast YES or NO votes on each candidate. When vote totals reach configured thresholds, the BBS can automatically validate or delete the account — or simply notify the SysOp to act manually.

> This is a community-driven vetting system, not a replacement for SysOp validation. Both can coexist: NUV handles the community vote; the SysOp retains final authority.

---

## How It Works

1. **New user registers** — if `autoAddNuv` is enabled, their handle is automatically placed in `data/nuv.json`.
2. **Eligible users are notified at login** (`CHECKNUV`) — if there are candidates they haven't voted on, a prompt appears.
3. **Users vote** via `SCANNUV` (or `LISTNUV` to review the queue first).
4. **Thresholds trigger** — when yes or no votes reach the configured limit, the BBS either acts automatically or logs a notice for the SysOp.

---

## Configuration

Turn NUV on and set its thresholds in the config editor: `./config` →
**Access & Security → New Users & Voting**.

| Field | Meaning |
|-------|---------|
| Use NUV | Enable the system. Cannot be on at the same time as **Auto Validate** on the same screen |
| Auto Add NUV | Queue every new signup automatically |
| NUV Use Level | Minimum access level to vote |
| NUV Yes Votes / NUV No Votes | Votes needed to trigger each threshold |
| NUV Validate | Validate the account automatically when the yes threshold is reached |
| NUV Kill | Soft-delete the account automatically when the no threshold is reached |
| NUV Level | Access level given on auto-validation |
| NUV Form | InfoForm number (1–5) shown to voters so they can judge the candidate; 0 shows none |

The `config.json` keys behind these fields are in the
[Technical Reference](#configjson-keys).

---

## Threshold Behaviour

### YES threshold reached (`nuvYesVotes`)

| `nuvValidate` | Result |
|---|---|
| `true` | User's access level set to `nuvLevel`, `validated = true`; candidate removed from queue |
| `false` | Log message only: `NUV: '<handle>' reached YES threshold — notify SysOp to validate`; candidate removed from queue |

### NO threshold reached (`nuvNoVotes`)

| `nuvKill` | Result |
|---|---|
| `true` | User's account soft-deleted (`deletedUser = true`); candidate removed from queue |
| `false` | Log message only: `NUV: '<handle>' reached NO threshold — notify SysOp to delete`; candidate removed from queue |

In both cases the candidate is removed from the queue once a threshold is reached — regardless of the auto-act setting.

---

## Menu Commands

### `RUN:CHECKNUV` — Login notification

Runs automatically in the login sequence. If the current user's access level is `>= nuvUseLevel` and there are candidates they haven't voted on, they see:

```
New User Voting: 3 candidate(s) awaiting your vote!
Vote now? [Y/N]:
```

Pressing `Y` immediately starts a scan of unvoted candidates. Pressing `N` continues the login sequence. If there are no unvoted candidates (or `useNuv` is disabled), this step is completely silent.

**Login sequence entry:**
```json
{"command": "CHECKNUV"}
```

This is included in the default `configs/login.json`. If upgrading an existing deployment, add it via `./config` → Login Sequence (key A).

---

### New user notification

When `autoAddNuv` is enabled, new users see a message after registration:

```
Your account has been submitted for community review.
```

---

### `RUN:SCANNUV` — Vote on pending candidates

Steps through all candidates the user hasn't yet voted on. For each:

```
New User Voting — Candidate #1
──────────────────────────────────────────────────
Voting for : JohnDoe
Yes votes  : 2
No votes   : 1
Added      : 01/15/2026

Voter Comments:
────────────────────────────────────────
SysOp               Yes Seems legit           01/15/26
OldTimer            No  No referral            01/16/26

[Y]es  [N]o  [C]omment  [R]eshow  [Q]uit:
```

| Key | Action |
|-----|--------|
| `Y` | Cast or change vote to YES |
| `N` | Cast or change vote to NO |
| `C` | Add or update a comment on your vote (must vote first; max 80 characters) |
| `R` | Redisplay the candidate stats |
| `Q` / Esc | Quit scan (remaining candidates not shown) |

The scan stops after the user quits or after all unvoted candidates have been shown. Voted-on candidates are not shown again unless the user revisits via `SCANNUV`.

**Default menu binding:** `H` or `NUV` on the Main Menu.

---

### `RUN:LISTNUV` — View the full candidate queue

Displays a table of all pending candidates with their current vote tallies and whether the current user has voted on each:

```
New User Voting Queue — 3 Candidate(s)
────────────────────────────────────────────────────────────
#    Handle               Added        Yes   No Voted?
────────────────────────────────────────────────────────────
1    JohnDoe              01/15/26        2    1 Yes
2    CoolUser99           01/16/26        0    0 No
3    RetroFan             01/17/26        3    2 Yes
```

For non-SysOp users this is a read-only view. Use `SCANNUV` to vote.

**SysOp management (access level 255):** SysOps see an interactive hotkey prompt instead of a static list:

```
[A]dd  [R]emove #  [V]ote on #  [Q]uit:
```

| Key | Action |
|-----|--------|
| `A` | Prompt for a handle, verify the user exists, add them to the NUV queue |
| `R` | Prompt for a candidate number, remove them from the queue |
| `V` | Prompt for a candidate number, open the voting screen for that candidate |
| `Q` / Esc | Return to the previous menu |

After each action the candidate table is refreshed automatically.

**Default binding:** Admin Menu `U` key.

---

## SysOp Access

The Admin Menu includes `U` → `LISTNUV` for reviewing and managing the queue. SysOps can add or remove candidates, vote directly from the list, and use `SCANNUV` from any menu to cast votes. All threshold-triggered actions (auto-validate, auto-delete) and SysOp queue removals are logged to the application log.

To manually manage a user affected by NUV thresholds when auto-action is disabled, use the Admin Menu `V` (Validate) or `E` (Edit Users) screens.

---

## Manually Adding a Candidate

SysOps can add a user to the NUV queue directly from `LISTNUV` by pressing `A` and entering the handle.

Alternatively, edit `data/nuv.json` and add an entry to the `candidates` array. The `votes` array can be empty.

---

## Technical Reference

### `config.json` keys

```json
{
  "useNuv": true,
  "autoAddNuv": true,
  "nuvUseLevel": 25,
  "nuvYesVotes": 5,
  "nuvNoVotes": 5,
  "nuvValidate": true,
  "nuvKill": false,
  "nuvLevel": 25,
  "nuvForm": 0
}
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `useNuv` | bool | `false` | Enable the NUV system. If `false`, all NUV commands are silent no-ops |
| `autoAddNuv` | bool | `false` | Automatically queue new users after registration |
| `nuvUseLevel` | int | `25` | Minimum access level required to vote |
| `nuvYesVotes` | int | `5` | Number of YES votes needed to trigger the yes threshold |
| `nuvNoVotes` | int | `5` | Number of NO votes needed to trigger the no threshold |
| `nuvValidate` | bool | `true` | If `true` and yes threshold is reached, automatically validate the user and set their level to `nuvLevel`. If `false`, log a notice for the SysOp to act manually |
| `nuvKill` | bool | `false` | If `true` and no threshold is reached, automatically soft-delete the user. If `false`, log a notice for the SysOp to act manually |
| `nuvLevel` | int | `25` | Access level assigned when a user is auto-validated via NUV |
| `nuvForm` | int | `0` | InfoForm number (1–5) displayed to voters; `0` = none |

### Data File (`data/nuv.json`)

Candidates are stored in `data/nuv.json`:

```json
{
    "candidates": [
        {
            "handle": "JohnDoe",
            "when": "2026-01-15T18:30:00Z",
            "votes": [
                {
                    "voter": "SysOp",
                    "yes": true,
                    "comment": "Seems legit",
                    "votedAt": "2026-01-15T19:00:00Z"
                },
                {
                    "voter": "OldTimer",
                    "yes": false,
                    "comment": "No referral",
                    "votedAt": "2026-01-16T10:15:00Z"
                }
            ]
        }
    ]
}
```

The file is created automatically when the first candidate is added. It is safe to edit manually (e.g., to remove a stuck candidate or pre-populate votes).

---

## See Also

- [Login Sequence](users/login-sequence.md) — configuring `CHECKNUV` in `login.json`
- [Admin Menu](users/admin-menu.md) — `U` key for NUV queue review
- [User Management](users/user-management.md) — manual validation and soft-delete
- [Configuration](configuration/configuration.md) — `useNuv`, `nuvYesVotes`, and related fields
