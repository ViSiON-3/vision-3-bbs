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

## Coming from a version older than v0.9.6

The worked example below covers one step, v0.9.6 to v0.9.7. Upgrade notes are
cumulative: coming from further back, work through each release you are
skipping, oldest first, before the one below. The notes for v0.9.0 to v0.9.3
are in the guide as it stood at v0.9.3, and the later ones in the guide as it
stood at each release:

| Upgrading to | What needs doing by hand |
| ------------ | ------------------------ |
| [v0.9.0](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v090) | Replace `bin/binkd`, which was a broken build; add two steps to `login.json`; copy the menu set |
| [v0.9.1](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v091) | Anonymous posting is off by default; new file-menu commands in `FILEM.CFG` |
| [v0.9.2](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v092) | Custom `login.json`: put `SYSOPNOTICES` and `NMAILSCAN` first |
| [v0.9.3](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v093) | Two or more FTN networks: each needed its own binkd outbound, which v0.9.4 now does for you |
| [v0.9.4](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.4/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v094) | Run `v3mail readdress --all` once; copy the changed menu files; repoint `GOTO:USERCFG` and removed `CFG_*` commands at `RUN:USERCONFIG` |
| [v0.9.5](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.5/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v095) | Copy `templates/configs/badusers.txt` into `configs/`; copy the changed menu files; review `configs/blocklist.txt`, because past auto-blocks now take effect |
| [v0.9.6](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.6/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v096) | Copy the changed menu files and `wfc` with the BBS; set `QuotePrefix` to ` ^I> ` if you want the new quote style, as it is not switched for you |

Where those notes say to restart after a config edit, see
[Restarting](#restarting) instead: most files have reloaded on save since
v0.9.2.

## Worked example: upgrading to v0.9.7

Deploy the new programs, run `./vision3 doctor`, and restart as usual. Nothing
in `configs/` has to change: every new setting has a default that keeps the
board as it was, and the V3Net dedup database is migrated the first time the
new version opens it. On an instance or bundle install, copy the menu files
below. The rest of this section is behaviour that changed and is worth
knowing about.

`bin/binkd`, `bin/sexyz` and the `wfc` console are unchanged.

v0.9.7 calls itself v0.9.6. The release shipped before its version number
was bumped, so everything that shows the version still says 0.9.6: `./vision3
doctor`, `wfc`, `helper` and `v3mail`, the tearline on posts your board sends,
your Central listing and door drop files. Go by the release tag or the git
commit you built from, not the banner. Later builds show the right number.

### 1. Bring the menu set up to date

A **repo-in-place** install gets these from `git pull`. An **instance** or
**bundle** install does not update `menus/`; copy these from the repo's
`menus/v3/` or the extracted bundle, skipping any you have customised in
`menus.d/`:

| Files | Why |
| ----- | --- |
| `ansi/MSGMENU.ANS`, `cfg/MSGMENU.CFG` | New **F** (Find Msgs) on the message menu, running `SEARCHMSGS A` over every conference |
| `cfg/MAIN.CFG` | New `/V CENTRAL` command, which opens V3Net Central |
| `ansi/MAIN.ANS` | Redrawn |
| `ansi/FASTLOGN.ANS` | Redrawn login screen |
| `ansi/CHATHEADER.ANS` | The empty bracket at the end of the header row is now a link field: `■` while the hub answers, `!` while it does not, blank in local chat |
| `theme.json` | New `autocorrectHighlightColor` (default `15`); without it the editor uses `15` anyway |

If you have your own message menu, add a command for `RUN:SEARCHMSGS` to get
message search: no data searches the current conference, `C` the current
area and `A` every conference. See
[Searching Messages](messages/message-areas.md#searching-messages).

### What else changed

- **V3Net Central, the board directory, is new and off.** Each subscription
  gets two switches: **Central**, which lets your callers browse the
  directory, and **Publish Listing**, which lists your board in it. Both
  default to `N`, and nothing about your board or callers is sent until you
  turn them on. The directory needs a v0.9.7 hub; on an older hub the
  subscription's Central status says the hub does not serve it. See
  [V3Net Central](v3net/central.md).
- **Message search.** `SEARCHMSGS` searches subjects and bodies with words,
  `"quoted phrases"`, `AND`, `OR`, `NOT` and parentheses. Esc stops a long
  search, and only the first matches are listed when there are many.
- **Optional autocorrect in the message editor.** It fixes common typos as
  a caller types, and is off unless you turn it on: board-wide with
  **Autocorrect** under **System Setup → Default Settings**
  (`editorAutocorrect`), or per area with the area's **Autocorrect** field.
  Callers can turn it off for themselves with **Ctrl+O** or in Konfig. See
  [Autocorrect](messages/message-areas.md#autocorrect).
- **`vision3 doctor` and the BBS check every ACS string.** Doctor's new
  **Access control** section names the file, record, field and column of
  each problem. A string that cannot be read at all is a **FAIL**: it was
  already denying every caller. A string that works, but not as it reads,
  is a **WARN**, such as an unknown letter, which is always false. Fix the
  warnings now: a future, stricter ACS check will change what those strings
  do. The BBS logs the same problems at startup and on reload, and never
  changes a string or refuses to start. See
  [Checking your strings](reference/acs.md#checking-your-strings).
- **ACS gains comparisons, ranges and user-record terms**, such as `S>=20`,
  `CALLS>=10`, `POSTS>0` and `AGE>=7`, and there is an ACS tester in `./ue`
  and the admin user editor. Existing strings mean what they did. See the
  [ACS reference](reference/acs.md).
- **Doors can be gated by ACS.** A door may now carry an `acs` string, set
  with the **ACS** field in `./config`, as well as `min_access_level`; a
  caller needs both. Leave it blank and nothing changes. A door's ACS is
  held to a stricter rule than other strings: any problem doctor reports
  denies the door to everyone, sysops included. See
  [Doors](doors/doors.md).
- **Session placeholders work in `strings.json`.** Values now expand the same
  `|XX` codes as menu prompts (`|UH`, `|TL`, `|MN`, `|NODE` and so on) and
  `|{ … |}` groups, and a new `|XYcc,rr` code moves the cursor. A custom
  string that happens to contain one of these codes as plain text now
  expands it; double the pipe (`||UH`) to print it as text. Text inserted
  into a string, such as a handle or a subject, is never expanded. The
  string editor gains a colour picker on **F2**.
- **Script doors are bounded.** A `v3_script` door now has a run time limit
  you can set per door (**Max Run Mins**; unset keeps the 30-minute limit,
  `-1` turns it off), and calls that pass or return too much data throw an
  error naming the limit instead of running unbounded. Scripts can read
  files and ZIP archives from a file area and tag files for download, and
  `v3.console.readkey` reads one key. `v3.file` listings no longer show
  areas the caller cannot list.
- **Marking a file in the file list checks the download rules.** Space in
  the lightbar file list now applies the area's download ACS and the batch
  size limit, as the download command does, and shows the reason when it
  refuses.
- **Network chat fixes that need the hub upgraded.** Private messages now go
  only to the recipient's board, instead of to every board on the network,
  which then filtered them. `/msg bob hello`, with no `@node`, now finds
  `bob` among the people in chat, and asks which board when more than one
  has that handle. Both are done by the hub: until it is upgraded, use the
  explicit `/msg bob@<node-id>` form. Callers stay in their rooms across a
  reconnect, chat lines wrap at the screen edge, and history shows when each
  line was said.
- **V3Net no longer imports your own posts back, or skips messages around
  them.** A post that failed to send, or was polled back while it was being
  sent, could arrive as a second copy, and a poll could miss another
  board's message posted just before yours. An import cut short by a crash
  is neither written twice nor skipped.
- **A long network address stays whole in message headers.** When the From
  or To field is too narrow, the name is shortened instead:
  `From: Dobbie Th... (3:633/2744.1)`.
- **FTN packets keep room for the NUL.** A 36-character name or 72-character
  subject made a packet the BBS's own reader rejected; those fields are now
  cut one character shorter.
- **Doctor names the menu file it read.** A problem in an override in
  `menus.d/` is reported against that file, not the shipped one under
  `menus/`.
