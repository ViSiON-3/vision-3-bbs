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

## Coming from a version older than v0.9.5

The worked example below covers one step, v0.9.5 to v0.9.6. Upgrade notes are
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

Where those notes say to restart after a config edit, see
[Restarting](#restarting) instead: most files have reloaded on save since
v0.9.2.

## Worked example: upgrading to v0.9.6

Deploy the new programs, run `./vision3 doctor`, and restart as usual. Nothing
in `configs/` has to change: every new setting has a default, and stored data
(users, the V3Net hub and dedup databases) is migrated the first time the new
version opens it. On an instance or bundle install, copy the menu files below.
The rest of this section is behaviour that changed and is worth knowing about.

`bin/binkd` and `bin/sexyz` are unchanged. Copy `wfc` along with the BBS:
choosing and switching chat screens with **Alt-S** needs both updated. An
older console against the new BBS still works, and chats on the default
screen.

### 1. Bring the menu set up to date

A **repo-in-place** install gets these from `git pull`. An **instance** or
**bundle** install does not update `menus/`; copy these from the repo's
`menus/v3/` or the extracted bundle, skipping any you have customised in
`menus.d/`:

| Files | Why |
| ----- | --- |
| `ansi/MAIN.ANS`, `ansi/FILEM.ANS`, `ansi/DOORSM.ANS`, `ansi/MSGMENU.ANS` | Redrawn; the message menu now shows **T** for the Twit List |
| `cfg/MSGMENU.CFG` | Binds **T** to the Twit List to match the art; **I** still works |
| `ansi/V3CHAT1.ANS`, `cfg/V3CHAT1.json` | New: the default split-screen chat screen |
| `ansi/V2CHAT1.ANS`, `cfg/V2CHAT1.json`, `ansi/V2CHAT2.ANS`, `cfg/V2CHAT2.json` | New: ViSiON/2's two chat screens, picked with **Alt-S** in the WFC console |
| `theme.json` | New `sysopChatScreen`, set to `V3CHAT1` |
| `templates/message_headers/MSGHDR.1.ans`, `.2.ans`, `.8.ans` | Styles 2 and 8 now show the message total; style 1 shows the reply counts |

`templates/USERLIST.icy` was unused and is gone from the set; delete your copy
or leave it. Without `sysopChatScreen` in your `theme.json`, sysop chat keeps
the built-in layout it had before. You can also set it from the config
editor's new **C. Theme** page, which writes the overlay copy in `menus.d/`.
See [Chat screens](how-to-guides/wfc-console.md#chat-screens).

### What else changed

- **Three wrong passwords drop the call.** The third wrong password on one
  call shows `NICETRY.ANS` and hangs up, as BBSes traditionally did, instead
  of drawing the login screen again. The count is `maxLoginTries` in
  `config.json` (default `3`), separate from `maxFailedLogins`, which still
  locks the IP out. An IP that is already locked out is now dropped rather
  than kept at the prompt.
- **Pipes are refused in new handles and real names.** A handle like `|CL`
  used to run its screen code on other callers' screens. Existing accounts
  keep their names and can still log in. Text that callers, hubs or other
  boards set is now escaped wherever it is shown, so only its colour codes
  act. This applies to anything passed into a `strings.json` format as well:
  a `|CR` in a conference name now prints as text. See
  [Pipes in Names](users/user-management.md#pipes-in-names).
- **Message text is filtered on the way to the screen.** Bodies, subjects,
  senders and recipients no longer pass terminal queries, window titles, mode
  changes or C0/C1 controls to the caller; colour and erase-in-line still do.
  A plain message that moves the cursor or clears the screen is now drawn as
  ANSI art, at 80 columns without word wrap. Stored messages are unchanged.
  Offline QWK packets get the same filter, and scripts get a new `text` field
  alongside the raw `body`.
- **Reply threads in the reader.** **T** in the reader now opens a thread
  menu in a pane under the header, instead of searching by subject, and the
  message list opens threaded; **T** in the list switches back to flat. Each
  caller can turn the pane off with **Thread View** in Konfig. See
  [Listing Messages](messages/message-areas.md#listing-messages).
- **Quotes reflow in the reader**, and the editor's Backspace and Delete
  refill a paragraph from its top line. New boards quote with a leading
  space; existing boards keep their own `QuotePrefix`, as described under
  [Changed shipped strings](#changed-shipped-strings).
- **Toasts.** Node pages reach a caller wherever they are, as a line on the
  bottom row, and a page they don't see before logging off waits for their
  next login. Logon and logoff announcements are available but off: turn on
  `toastLogons` / `toastLogoffs` under **System Setup → Toasts** in
  `./config`. See [Toasts](users/login-sequence.md#toasts).
- **The config editor moved some items.** **7. Door Programs** is now a
  sub-menu holding **Doors** and **Door Categories**; the separate **C. Door
  Categories** item is gone, and **C** is now **Theme**. WFC Access and the
  page settings moved to **Access & Security → WFC & Paging**, and
  **New User Voting** is now **New Users & Voting**.
- **Screen width is at least 80.** A caller's stored width under 80 is raised
  to 80 when `users.json` loads, and Konfig accepts 80 to 255.
- **V3Net messages and hub replies are signed.** Messages carry their
  author's signature, the hub signs its replies, and a leaf pins the
  coordinator and hub keys on first contact, writing them to `v3net.json`.
  All of it works with older hubs and leaves, and every "require" setting is
  off, so hubs and leaves can upgrade in any order. If a pinned key later
  changes, sysops get a login notice until the new key is accepted. A changed
  coordinator key gets its NAL rejected, and the last accepted NAL stays in
  effect; a changed hub key pauses syncing with that network. A coordinator
  moving the network to a new key should use a signed handover, which leaves
  follow without the sysop. See
  [Coordinator Key Pinning](v3net/nal.md#coordinator-key-pinning) and
  [Hub signatures](v3net/configuration.md#hub-signatures).
- **V3Net hubs refuse bad names and text.** A hub now refuses a subscribe
  whose BBS name has pipes or control characters or is over 64 characters,
  and a NAL or proposal with such text in an area. Updated leaves clean their
  own board name before sending it, and a node already registered can
  re-subscribe under the name the hub holds.
- **The QWK API pairs devices.** A password login now only pairs a device;
  packets need a device login, which signs a challenge with a key kept on the
  phone. The API also follows the BBS blocklist, login lockouts and
  `logonLevel`, and a password change ends every session and unpairs every
  device. No mobile client had shipped, so nothing deployed breaks. Callers
  can list and unpair devices with **O** in Konfig. See
  [Paired devices](messages/qwk-api.md#paired-devices).
- **FidoNet points leave SEEN-BY and PATH alone.** A point used to stamp its
  boss's address into both, which hubs could reject as a loop. Headers of
  mail labelled `CHRS: CP866` (and CP1251, KOI8-R, KOI8-U) are now decoded
  and exported in that charset; headers stored before the upgrade stay as
  they were.
