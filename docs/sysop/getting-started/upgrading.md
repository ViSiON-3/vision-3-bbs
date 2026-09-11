# Upgrading

Upgrading ViSiON/3 replaces the programs. Followed as written, none of the
procedures below touch your settings in `configs/` or your `data/` — which is
what makes upgrading safe, and also what makes two steps necessary:

- Settings added since your version are not applied to your existing config
  files.
- Your menu set is a different story, and which story depends on how you
  installed. A repo-in-place upgrade **will** update `menus/`, and can
  overwrite menu edits you made in the repo. An instance or bundle install
  never updates it, so artwork fixes do not reach you at all.
- The binaries in `bin/` (`binkd`, `sexyz`) are prebuilt — a source build
  (`git pull` + `build.sh`) never touches them, so they stay at the version you
  first installed. Most releases don't change them; when one does, the release
  notes and the worked example below say so. **v0.9.0 is one that does** — its
  binkd fixes a broken build, and every install has to take the new one.

Nothing warns you about any of these, so this is the part of an upgrade worth
reading.

## Which install do you have

| Layout | How to tell |
| ------ | ----------- |
| **Repo in place** — you run the BBS from the git clone | `.git`, `cmd/` and `configs/` are all in one directory |
| **Instance + repo** — built by `dev-setup.sh` into a separate directory | The BBS directory has `configs/` and `data/` but no `cmd/`; its programs may be symlinks |
| **Release bundle** — extracted `vision3-bundle-*` | No `.git` and no `cmd/` anywhere |

To check an instance, look at whether the programs are symlinks:

```bash
ls -l /opt/vision3/vision3
# -> /home/you/git/vision3/vision3  means symlinked; a plain file means copied
```

## Before you start

Take the BBS down, and copy these somewhere safe:

```text
configs/      your settings — the thing an upgrade must not lose
data/         users, messages, files, logs
menus/        if you have edited any menu, .ANS or .CFG file
```

Two ways to lose work, both avoidable:

- **`menus/` on a repo-in-place install.** The menu set is version-controlled,
  so a pull can overwrite edits you made in place. This one bites even when you
  do everything else right.
- **`configs/` and `data/` if you extract a release bundle over your existing
  directory.** A bundle contains both, so extracting in place replaces your
  settings and can overwrite your data. The bundle section below says how to
  avoid this; it is the single most destructive thing you can do while
  upgrading.

Follow the procedures below and the backups are insurance. Deviate from the
bundle one and they are the only thing between you and starting over.

## Repo in place

```bash
git pull
./build.sh          # or build.ps1 / build.bat on Windows
```

`configs/` is git-ignored, so `git pull` cannot touch your settings.

**`menus/` is tracked**, so a pull does update the shipped menu set — and will
conflict, or overwrite, if you have edited a menu, `.ANS` or `.CFG` file in
place. Commit your menu edits to a branch, or keep copies outside the repo,
before pulling.

Re-running `./setup.sh` is safe: it copies a config template only when the
target does not already exist. It fills in genuinely new files and leaves
everything else alone. It does not merge new keys into files you already have.

## Instance + repo (`dev-setup.sh`)

Update the repo, then rebuild:

```bash
cd ~/git/vision3
git pull
./build.sh
```

If the instance's programs are **symlinks** (`--symlink`), that is all — they
already point at the rebuilt binaries. If they were **copied**, copy them again:

```bash
cd /opt/vision3
for b in vision3 helper v3mail strings ue config menuedit wfc; do
  src=~/git/vision3/$b
  [[ -f $src ]] || { echo "missing from the repo: $b"; continue; }
  cp "$src" . || echo "FAILED to copy: $b"
done
```

Do not silence that loop with `2>/dev/null`. A copy that fails on permissions
leaves the old program in place, and an upgrade that half happened is worse
than one that visibly did not.

**Your `menus/` does not track the repo.** `dev-setup.sh` copies the menu set
once, when the instance is created, and skips it on every later run — regardless
of `--symlink`, which only ever applies to the programs. So menu and artwork
fixes never arrive on their own. Either copy the changed files by hand:

```bash
cp ~/git/vision3/menus/v3/ansi/SOMEFILE.ANS /opt/vision3/menus/v3/ansi/
```

…or, if you have not customised the menu set, replace the directory with a
symlink once and it will track the repo from then on:

```bash
cd /opt/vision3 && rm -rf menus && ln -s ~/git/vision3/menus menus
```

The same is true of `configs/`: templates are copied only when the file is
absent, so see [Settings added since your version](#settings-added-since-your-version).

## Release bundle

**Do not extract a new bundle over your existing directory.** A bundle is a full
distribution — it contains `configs/`, `menus/` and a `data/` skeleton — so
extracting it in place will overwrite your settings and your menu set.

Extract to a scratch directory, then copy the programs across:

```bash
mkdir -p /tmp/v3new
tar xzf vision3-bundle-linux-amd64-vX.Y.Z.tar.gz -C /tmp/v3new
cd /opt/vision3
cp /tmp/v3new/{vision3,ue,strings,config,menuedit,helper,v3mail} .
cp /tmp/v3new/bin/{binkd,sexyz} bin/    # the bundle carries these; a source build does not
```

Then compare `/tmp/v3new/configs/` against your own, as below, and copy across
any menu or artwork files you have not customised.

A bundle is the simplest way to get the new `bin/binkd` this release requires:
it is already in the archive, so the `cp` above is all it takes.

## Settings added since your version

This is the step that gets missed. New settings ship in the templates, and
**templates are only ever copied to a config file that does not exist yet**. A
config file you already have is never modified, so a new setting simply is not
in it.

What that means depends on the file:

| File | A missing key means |
| ---- | ------------------- |
| `config.json` | The built-in default applies. Usually nothing to do. |
| `strings.json` | Usually a built-in fallback prints the shipped text. A key with no fallback prints nothing. |
| `login.json` | **Nothing happens.** A login step that is not listed does not run. |
| `doors.json`, `events.json` | Entries are lists, not settings; nothing is inherited. |

So `login.json` is the one that always needs a decision, and `config.json` is
usually informational — worth reading so you know a new setting exists, but it
is already doing something sensible.

For strings, `./strings` tells you which case you are in: an entry that is blank
but still prints its shipped text is marked differently from one that is
genuinely empty. You do not have to guess from the JSON.

To list what your files are missing, run this from your BBS directory, with
`TPL` pointing at the new version's templates — `templates/configs` in the repo,
or the extracted bundle's `configs`:

```bash
TPL=~/git/vision3/templates/configs

for f in config.json strings.json; do
  echo "== $f =="
  python3 -c "
import json
tpl=json.load(open('$TPL/$f'))
live=json.load(open('configs/$f'))
print('\n'.join('  '+k for k in tpl if k not in live) or '  (nothing new)')
"
done

echo "== login.json =="
python3 -c "
import json
tpl={i['command'] for i in json.load(open('$TPL/login.json'))}
live={i['command'] for i in json.load(open('configs/login.json'))}
print('\n'.join('  '+c for c in sorted(tpl-live)) or '  (nothing new)')
"
```

Add what you want in `./config` and `./strings`, or by editing the JSON
directly. There is no merge tool, and adding every new setting is not required —
the defaults above are chosen so that skipping this step still leaves a working
system.

## Restarting

Restart the BBS. A running `vision3` keeps executing the binary it started with,
so new programs do nothing until it does.

Once it is running, these reload on save with no restart:

- `strings.json`
- `login.json`
- `doors.json`
- `theme.json` (in the menu set)

`config.json` reloads too, but **not every setting in it takes effect**. Access
levels, new-user settings and the like apply immediately; ports, host keys and
the IP connection limits are read once at startup and keep their old values
until you restart. The BBS logs a reminder to that effect on every reload of
that file.

`events.json` needs a restart, because the scheduler is built at startup.

## Worked example: upgrading to v0.9.0

v0.9.0 is a large release, and it touches every category above — a prebuilt
binary, config keys, login steps and menu artwork. Work through it in this
order.

### 1. Replace `bin/binkd` — required for FidoNet

Every Unix binkd shipped before v0.9.0 was built with a broken MD5, so binkp
sessions that negotiated CRAM-MD5 failed against every peer, in both directions,
with a correct password. v0.9.0 fixes the build. Because `bin/binkd` is a
prebuilt binary a source build never rebuilds, you must take the new one
yourself:

- **Bundle upgrade:** already done — the `cp .../bin/{binkd,sexyz}` step above
  installed it.
- **Repo-in-place or instance upgrade:** copy `bin/binkd` from a v0.9.0 release
  bundle, or build it with `./scripts/build-binkd.sh --out bin/binkd` (it
  verifies the result against the RFC 2202 test vector and refuses to install a
  broken one).

If you had worked around the old bug, undo the workaround now, or CRAM-MD5 stays
off: set `disable_cram_md5` back to `false` under `ftn.binkd` in
`configs/ftn.json`, and drop any `-m` you added to the poll events in
`configs/events.json`. A repaired link logs `pwd protected session (MD5)`
instead of `(plain text)`. Windows binkd was never affected.

### 2. `config.json` — new keys, all with working defaults

Two keys were added; both default to something sensible, so the BBS runs
correctly with no edit. Read them so you know they exist:

- `notifySysopNewUser` (default `true`) — real-time SysOp page when a new user
  signs up.
- `autoValidateNewUsers` (default `false`) — when `true`, new users are granted
  their full access immediately instead of waiting for validation. Leave it off
  unless you want an open board.

### 3. `login.json` — two new steps you must add yourself

Neither runs unless you list it (a login step that is not present simply does
not execute):

```json
{ "command": "NEWUSERVAL", "sec_level": 255 }
```

Place `NEWUSERVAL` before `CHECKNUV`, using your own `sysOpLevel` if it is not
255. Without it the real-time new-user page still works; you lose only the
"N users pending" prompt at login.

```json
{ "command": "PRINTNEWS" }
```

`PRINTNEWS` shows System News at login. Add it wherever you want the news to
appear in the sequence; without it, news is reachable only from the menus.

### 4. `strings.json` — new text, mostly with fallbacks

v0.9.0 adds a batch of strings (new-user flow, file-area and batch messages,
conference prompts). Most carry a built-in fallback, so they print sensible
text with no edit; `./strings` marks which are genuinely empty versus falling
back. Add or reword only what you want to change — see the listing script above
to see exactly which keys your `strings.json` is missing.

### 5. Menu artwork and layout — copy for non-repo installs

v0.9.0 changed several menu files. A **repo-in-place** install gets them from
the pull (mind your own edits); an **instance or bundle** install needs each one
copied across by hand:

| File(s) | What changed |
| ------- | ------------ |
| `menus/v3/ansi/MAIN.ANS`, `cfg/MAIN.CFG`, `mnu/MAIN.MNU` | Main menu cleaned up; Newscan Pointers moved off it |
| `menus/v3/ansi/MSGMENU.ANS` | Corrected scan labels and a wrong hotkey (artwork advertised `[Z]` for an entry bound to `U`) |
| `menus/v3/ansi/DOORSM.ANS`, `cfg/DOORSM.CFG` | Doors menu now advertises only doors that actually run |
| `menus/v3/templates/message_headers/MSGHDR.*.ans` | Header templates: every one is now reachable, MSGHDR.3 shows the subject, and MSGHDR.9's title row is fixed |

Copy a screen's files as a set — the `.ANS`, `.CFG` and `.MNU`/template that
make up one menu must agree, so taking one and not the others can leave you
worse off than before.

### 6. Restart

Follow [Restarting](#restarting). The new `vision3` and the new `bin/binkd` both
take effect only once you restart.

## Worked example: upgrading to v0.9.1

v0.9.1 is a much lighter upgrade than v0.9.0 — most of it is either automatic
once you deploy the new binaries, or optional. The only things that need your
hand are edits to **customized** config and menu files, since an upgrade never
overwrites those. This section covers those; everything else just works after
you drop in the new bundle.

### Anonymous posting is off by default

Anonymous posting is now a per-area opt-in. Each message area has an **Allow
Anonymous** setting, and an area that has never set it is treated as **No** —
where before, anonymous posting was offered in every area to any user who met
the `anonymousLevel` access level.

Nothing in your config files changes on upgrade, and nothing needs migrating.
But if you had areas where users posted anonymously, that stops until you turn
it back on:

- In `./config` → **Message Areas**, edit each area that should allow it and set
  **Allow Anonymous** to `Y`.
- The access-level gate still applies on top: a user is offered the anonymous
  prompt only if they meet `anonymousLevel` **and** the area allows it.
- A conference can still veto it — an area set to `Y` inside a conference whose
  own Allow Anonymous is `N` stays off.

`message_areas.json` is read at startup, so restart the BBS after editing areas
for the change to take effect.

### New: optionally require new users to message the SysOp

Signup can now end by making the caller leave you a private message — the
classic "leave the SysOp feedback to finish registration" gate. It is **off by
default**, so nothing changes unless you turn it on.

- In `./config` → **System** → **Default Settings**, set **Require Email** to
  `Y` (or set `"requireNewUserEmail": true` in `config.json`).
- With it on, once an account is created the caller is shown `NUEMAIL.ANS`,
  paused, then dropped into the message editor addressed to the SysOp (user #1).
  The message lands in **Private Mail** like any other.
- **It cannot be skipped.** Aborting the editor (Ctrl-A) or saving an empty
  message re-prompts and returns them to the editor. The only ways out are to
  send a message or to drop the connection.
- **Dropping the connection doesn't dodge it.** The obligation is stored on the
  account, so on their next login they are sent straight back into the editor —
  even if their access level is below `logonLevel` and they otherwise couldn't
  get on yet. After **three** abandoned attempts (the signup plus two
  reconnects) the account is soft-deleted, and is removed for good by the usual
  deleted-user purge.
- Customize the screen by dropping a `NUEMAIL.ANS` into your menu set's `ansi`
  directory (a starter one ships with the `v3` set). With no file present, a
  configurable string (`newUserEmailPrompt`) is shown instead; the default
  subject line comes from `newUserEmailSubject`.

This pairs well with leaving `autoValidateNewUsers` off: the signup-time message
is your one chance to hear from a caller before you validate them, and the
requirement now follows them across reconnects until they either introduce
themselves or the account ages out.

### New: new-user notices reach an offline SysOp, and a "read mail now?" prompt

Two login-sequence quality-of-life changes. **Both require an edit to
`configs/login.json` if you maintain your own** (installs without the file get
them from the built-in default automatically):

- **`SYSOPNOTICES`** — the "new user signed up" notice (`notifySysopNewUser`)
  used to be a live page only, so it was lost whenever no SysOp was online at
  signup time (which is most of the time). It is now also **queued and shown at
  the SysOp's next login**. Add a `{"command": "SYSOPNOTICES"}` item to your
  login sequence — put it **first**, above `FASTLOGIN`, since a fast-login jump
  ends the sequence and would skip everything below it. It is informational
  and fires regardless of `autoValidateNewUsers` — unlike `NEWUSERVAL`, which is
  silent when nothing is pending validation. Queued notices live in
  `data/sysop_notices.json`. The wording is its own string,
  `newUserSysopNotice` — the live page's "just signed up" is not true of a
  notice read on the SysOp's next call, so the queued form states how long ago
  the signup happened. No strings.json edit is needed; the key has a built-in
  default.
- **"Read it now?" after `NMAILSCAN`** — when the login mail scan reports new
  private mail, the caller is now asked whether to read it immediately, dropping
  them into the reader (reply/skip per message). No config change needed beyond
  already having `NMAILSCAN` in your sequence.

### New: file-menu commands (`FILEM.CFG`)

The file/transfer menu gained commands that mirror the message menu. Installs
using the shipped `FILEM.CFG` get them automatically; if you maintain a custom
one, add the keys you want:

- `[C]` → `RUN:CHANGEFILECONF` — change file conference. Changing conference in
  either the file **or** message menu now sets it for both.
- `]` / `[` → `RUN:NEXTFILEAREA` / `RUN:PREVFILEAREA` — step through file areas.
- `}` / `{` → `RUN:NEXTFILECONF` / `RUN:PREVFILECONF` — step through conferences.
- `[Y]` → `RUN:SETFILESCANDATE` — set the file newscan cutoff (a date, all, or
  reset to "since last logon"). The message menu keeps this on `U`, but the file
  menu uses `U` for Upload.
- `[Z]` → `RUN:FILENEWSCANCONFIG` — set scan areas (previously unlabeled).

If you use a custom `FILEM.ANS`, refresh it — the shipped art now lists these
commands (and adds a rumor line). Menu art is read live, so no restart is needed
for an art change.

### No restart for `ftn.json` changes

The integrated mailer now re-reads `ftn.json` on its own. A config-editor save
of `export_interval_seconds` applies live; other binkd settings apply on binkd's
next (re)launch — and, importantly, the supervisor no longer overwrites a newer
`binkd.conf` with stale boot-time values. No action needed; it just stops
silently reverting your changes.

## Worked example: upgrading to v0.9.2

v0.9.2 is almost entirely automatic: deploy the new bundle, restart, and the
fixes and the new live config reload are active. The one thing that needs your
hand is a **customized `login.json`** — and even that is only an ordering edit.

### Custom `login.json`: put SYSOPNOTICES and NMAILSCAN first

Two things changed around the login sequence:

- `SYSOPNOTICES` now actually runs at login. It was silently dead — the login
  sequence dispatcher never knew the command, so queued "new user joined"
  notices piled up undelivered (you'd see `unknown login sequence command`
  warnings in the log). Any notices already queued are delivered on your next
  login after the upgrade.
- The shipped sequences now lead with `SYSOPNOTICES` then `NMAILSCAN`, **above**
  `FASTLOGIN`. A `FASTLOGIN` jump ends the sequence, so anything below it is
  skipped for callers who take the shortcut — which is how sysop notices and
  the new-mail scan were being missed.

An upgrade never touches your `login.json`, so if you maintain a custom one,
reorder it so `SYSOPNOTICES` and `NMAILSCAN` are the first two items, above
`FASTLOGIN`. (Earlier docs suggested adding `SYSOPNOTICES` after `NEWUSERVAL`,
which lands below `FASTLOGIN` — move it up.)

### Config edits now apply to the running BBS

Saving from `./config` (or editing a config file by hand) no longer needs a
restart for most files — see
[Configuration](../configuration/configuration.md) for the full live-vs-restart
table and how the `configs/reload.now` / `configs/reload.force` semaphore files
and `SIGHUP` work. Structural files (`file_areas.json`, `message_areas.json`,
`v3net.json` hand edits) are validated on save and applied the moment the board
is empty, so live callers are never yanked around. Nothing to migrate — this is
just a behavior change to know about.

### Stranded "already read" mail heals on the next pack

If mail ever showed as already-read without being opened (typically after the
nightly purge/pack emptied an area), that was a stale lastread pointer left on
the old message numbering by `Pack`. Packs now remap pointers, so the problem
stops recurring — and pointers stranded by *past* packs are clamped back into
range the next time that area is packed. `v3mail lastread` can reset one by
hand if you don't want to wait.

### Optional: hang up bot connections sooner

The challenge gate's stray-key limit (previously hard-coded at 8) is now
**Stray Keys** on the Bot Defense screen (`challengeGateStrayLimit` in
`config.json`, minimum 1). Existing configs without the key keep the old
behavior. Setting it to `1` drops a caller on the first wrong key — aggressive,
since callers who lean on Enter at connect get dropped too.

### Last callers list

Invisible logins are now hidden from **every** viewer (before, CoSysOp+ still
saw them), and they no longer crowd real callers off the screen — the list
always shows up to 20 visible callers. Real callers evicted from the old,
shallower history are gone for good; the screen refills as calls arrive. No
action needed.
