# Upgrading

Upgrading ViSiON/3 replaces the programs. Followed as written, none of the
procedures below touch your settings in `configs/` or your `data/` — which is
what makes upgrading safe, and also what makes two steps necessary:

- Settings added since your version are not applied to your existing config
  files.
- Your menu set is a different story, and which story depends on how you
  installed. A repo-in-place upgrade **will** update `menus/`, and can
  overwrite menu edits you made in the repo. An instance or bundle install
  never updates it, so artwork fixes do not reach you at all. Keeping your
  edits in `menus.d/` instead of `menus/` sidesteps both problems — see
  [Customising menus without losing your changes](menus/menu-system.md#customising-menus-without-losing-your-changes).
- The binaries in `bin/` (`binkd`, `sexyz`) are prebuilt — a source build
  (`git pull` + `build.sh`) never touches them, so they stay at the version you
  first installed. Most releases don't change them; when one does, the release
  notes and the worked example below say so.

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
place. Keep your edits in **`menus.d/`** instead: it is searched before `menus/`
file by file, it is git-ignored, and `menuedit` saves there by default. If you
already have edits inside `menus/`, move them across once:

```bash
# see which shipped files you have changed
git status --short menus/
# move each one into the overlay, then let git restore the shipped copy
mkdir -p menus.d/v3/ansi
mv menus/v3/ansi/MAIN.ANS menus.d/v3/ansi/
git checkout -- menus/v3/ansi/MAIN.ANS
```

After that, `git pull` cannot conflict on menus again. The full procedure,
including edits committed on a branch, is in
[Moving existing customisations into menus.d](menus/menu-system.md#moving-existing-customisations-into-menusd).

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

…or replace the directory with a symlink once and it will track the repo from
then on, with your own customisations kept in the instance's `menus.d/` where
the symlinked set cannot overwrite them:

```bash
cd /opt/vision3
diff -rq menus/v3 ~/git/vision3/menus/v3      # find what you changed or added
# move each of those files to the same path under menus.d/v3/, then:
rm -rf menus && ln -s ~/git/vision3/menus menus
```

`menus.d/` is searched before `menus/`, file by file, so the shipped set can
change underneath you without touching a file you have overridden. Step by
step, including how to tell your edits from fixes you have not copied yet:
[Moving existing customisations into menus.d](menus/menu-system.md#moving-existing-customisations-into-menusd).

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
cp /tmp/v3new/{vision3,ue,strings,config,menuedit,helper,v3mail,wfc} .   # wfc ships from v0.9.4
cp /tmp/v3new/bin/{binkd,sexyz} bin/    # the bundle carries these; a source build does not
```

Then compare `/tmp/v3new/configs/` against your own, as below, and copy across
any menu or artwork files you have not customised.

When a release changes `bin/binkd` or `bin/sexyz`, a bundle is the simplest
way to get the new one: it is already in the archive, so the `cp` above is all
it takes.

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

### Changed shipped strings

Sometimes a release changes the text of a string that already shipped, for
example to list a new key in a prompt. Your `strings.json` already has the key,
so the fallbacks above don't apply.

If your value is still exactly the old shipped text, you never edited it, and
the BBS uses the new text instead. `./strings` shows it as the default, and
saving from there writes it to your file. If you changed the string in any way,
even by one space, your version is kept. The release notes say when this
happens, so you can update your own text by hand.

`QuotePrefix` is the exception. Its shipped value went from `"^I> "` to
`" ^I> "`, so new quotes start with one space (` RW> text`), but it is not
switched for you even if you never edited it, because it changes the text your
board sends out in echomail and netmail. To take the new default, set **Quote
Prefix** in `./strings` to ` ^I> `, with the leading space, or edit
`configs/strings.json`. Your own prefix keeps working if you would rather leave
it. Either way the message reader shows every quote line with one leading space
and rewraps it to the caller's screen; stored messages are not changed.

## Check the board with doctor

Before restarting, run the new binary's health check from the BBS directory:

```bash
./vision3 doctor
```

It reads every config file, the menu set and the mail setup without starting
the board, and reports anything the new version is unhappy about: a config
file that no longer parses, a key the new version does not know (a typo an
older version silently ignored), a template file the upgrade expects you to
have, a world-readable file holding passwords. Each finding comes with a
`FIX →` line. It exits `0` when clean, `1` with warnings and `2` with
failures, so a deploy script can stop on `2`. See
[Doctor](configuration/configuration.md#doctor).

## Restarting

Restart the BBS. A running `vision3` keeps executing the binary it started with,
so new programs do nothing until it does.

Once it is running, most configuration changes need no restart. Saving from
`./config`, or editing a config file by hand, is picked up within a couple of
seconds. Changes to message areas, file areas and `v3net.json` wait until no
callers are online. Some settings still need a restart, among them listening
ports and hosts, SSH host keys, turning SSH or telnet on or off, and the
logging directory and rolling settings. The full list is under
[Applying Configuration Changes](configuration/configuration.md#applying-configuration-changes).

## Coming from a version older than v0.9.4

The worked example below covers one step, v0.9.4 to v0.9.5. Upgrade notes are
cumulative: coming from further back, work through each release you are
skipping, oldest first, before the one below. The notes for v0.9.0 to v0.9.3
are in the guide as it stood at v0.9.3, and the v0.9.4 notes as it stood at
v0.9.4:

| Upgrading to | What needs doing by hand |
| ------------ | ------------------------ |
| [v0.9.0](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v090) | Replace `bin/binkd`, which was a broken build; add two steps to `login.json`; copy the menu set |
| [v0.9.1](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v091) | Anonymous posting is off by default; new file-menu commands in `FILEM.CFG` |
| [v0.9.2](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v092) | Custom `login.json`: put `SYSOPNOTICES` and `NMAILSCAN` first |
| [v0.9.3](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v093) | Two or more FTN networks: each needed its own binkd outbound, which v0.9.4 now does for you |
| [v0.9.4](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.4/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v094) | Run `v3mail readdress --all` once; copy the changed menu files; repoint `GOTO:USERCFG` and removed `CFG_*` commands at `RUN:USERCONFIG` |

Where those notes say to restart after a config edit, see
[Restarting](#restarting) instead: most files have reloaded on save since
v0.9.2.

## Worked example: upgrading to v0.9.5

Deploy the new programs, run `./vision3 doctor`, and restart as usual. Then
copy `configs/badusers.txt` into place, and on an instance or bundle install
copy the few menu files below. The rest of this section is behaviour that
changed and is worth knowing about.

`bin/binkd` and `bin/sexyz` are unchanged. Copy `wfc` along with the BBS:
snoop, type-in and split-screen chat need both updated. See
[WFC Console](how-to-guides/wfc-console.md).

### 1. Add `configs/badusers.txt`

Signup now refuses the handles listed in `configs/badusers.txt`. A new install
gets the template; an upgraded board does not, and `vision3 doctor` warns
*bad user names ... is missing* until it does. Copy
`templates/configs/badusers.txt` into `configs/` (it blocks `root`, `admin*`,
`*sysop*`, `guest` and `anonymous`) and add your own. The path is
`badUsersPath` in `config.json`, a sixth item, **Bad User Names**, under
Access & Security in `./config`. See
[Bad user names](configuration/configuration.md#bad-user-names).

### 2. Bring the menu set up to date

A **repo-in-place** install gets these from `git pull`. An **instance** or
**bundle** install does not update `menus/`; copy these from the repo's
`menus/v3/` or the extracted bundle, skipping any you have customised in
`menus.d/`:

| Files | Why |
| ----- | --- |
| `cfg/MSGMENU.CFG`, `ansi/MSGMENU.ANS` | New **T** key, Twit List (`RUN:CFG_TWITLIST`); **I** is kept as an alias |
| `ansi/SPONSORM.ANS`, `ansi/CHATHEADER.ANS`, `ansi/YOURSTAT.ANS` | Redrawn or corrected; the chat header gains the Users and Mentions counters |
| `theme.json` | New `chatSysopColor` and `chatUserColor` for split-screen chat |
| `mnu/DOORMENU.MNU`, `templates/DOORMENU*`, `templates/DOORCAT.*` | New: the [generated door menu](doors/generated-menu.md), needed only if you bind `RUN:DOORMENU` |
| `mnu/TWITLIST.MNU`, `templates/TWITLIST.*` | New: the Twit List screen. Optional; missing files fall back to built-in copies |

To put the Twit List on a customised message menu, add:

```json
{
    "KEYS": "I",
    "CMD": "RUN:CFG_TWITLIST",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Editing Twit List"
}
```

It is also item **M** of the user **K** editor. Twit Lists are on unless
`twitListEnabled` is `false` in `config.json`. See
[Twit List](messages/twit-list.md).

### What else changed

- **Auto-blocked IPs now block.** An IP locked out for failed logins has
  always been appended to your blocklist file with an `# auto-blocked`
  comment, but the comment made the loader skip the line, so the block was
  lost at the next reload. Those lines are now honoured. Review
  `configs/blocklist.txt` after upgrading: every IP auto-blocked in the past
  is blocked for real from now on. Remove the line to unblock one.
- **Encoding is detected at connect.** A caller whose encoding is unset is
  now on Auto and detected every call, and the first-login UTF-8/CP437
  prompt is gone. A caller who saved `CP437` on a UTF-8 terminal before this
  release keeps that saved value and sees garbled art; tell them to open
  **K** and press **Encoding** until it reads `Auto (...)`. See
  [Encoding](advanced/encoding.md).
- **Scheduled events wait instead of being dropped.** An event that fired
  while every slot was busy used to be skipped with a warning. It now waits
  for a slot, for up to its timeout, so a nightly purge that shared a minute
  with the hub polls finally runs. Skips are counted in the event history
  and the WFC Events tab. The template's nightly sequence changed too: the
  fix runs at `3 2 * * *` and purge and pack follow it with `run_after`. If
  you enabled the older examples, compare yours with
  [Nightly Message Base Maintenance](advanced/event-scheduler.md#nightly-message-base-maintenance).
- **Purge limits that nobody enforces are reported.** With `max_messages` or
  `max_age` set on any area and no enabled event running `v3mail purge`, the
  BBS logs a warning at startup and `./config` notes it when you set a limit.
  If your areas have grown past their limits, this is why.
- **Saves no longer overwrite each other.** `./config`, `helper` and the BBS
  lock the shared config files and merge their changes, so a conference added
  by `helper ftnsetup` survives a save in the editor you left open. You will
  see `<file>.lock` sidecars appear in `configs/` while something is writing;
  they are harmless and git-ignored. The editor now logs to
  `data/logs/config.log`.
- **Quote banners.** A `strings.json` without `quoteTopNet` gets `^N wrote:`
  above quoted network text; a key that is present but blank means no
  banner. The shipped local quote banners are now `[^N Said]` and
  `[^N Done]`; an existing board keeps its own. `konfigAutoSigChoice` is
  upgraded automatically unless you edited it, and `twitListEnabled` absent
  from `config.json` means Twit Lists are on.
- **Strings can run scripts or show files.** A `strings.json` value starting
  with `!CODE` runs a `v3_script` door in place of its text, and one starting
  with `%file.ext` shows an ANSI file first. Nothing changes until you use
  it. The user **K** form's text now comes from new `konfig*` strings, all
  with fallbacks. See [String Editor](advanced/string-editor.md).
- **IPv6 listener hosts.** `sshHost`, `telnetHost` and the V3Net hub host
  accept `::`, `::1` or a bracketed form. Nothing to do unless you want it.
- **FTN own address follows into `binkd.conf`.** Correcting a network's own
  address in the wizard or under Echomail Networks now rewrites the matching
  `address` line in `binkd.conf`, which used to need a hand edit.
