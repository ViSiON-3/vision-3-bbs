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

## Coming from a version older than v0.9.7

The worked example below covers one step, v0.9.7 to v0.9.8. Upgrade notes are
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
| [v0.9.7](https://github.com/ViSiON-3/vision-3-bbs/blob/53393b7c8cb7e6043fa553a77af6ad4cc86f2818/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v097) | Copy the changed menu files; fix what `./vision3 doctor` warns about in ACS strings, because v0.9.8 turns those warnings into failures |

Where those notes say to restart after a config edit, see
[Restarting](#restarting) instead: most files have reloaded on save since
v0.9.2.

## Worked example: upgrading to v0.9.8

Two things need doing before you deploy: clear the ACS warnings your current
`./vision3 doctor` reports, and, on a V3Net network, check that the hub and
the other boards are new enough to sign. Then deploy the new programs, run
`./vision3 doctor`, and restart as usual. Nothing in `configs/` has to
change, and on an instance or bundle install you copy the menu files below.

`bin/binkd` and `bin/sexyz` are unchanged. A source build now needs
**Go 1.27.4**; with Go's default `GOTOOLCHAIN=auto`, an older installed Go
downloads it for you.

### 1. Fix ACS warnings before you upgrade

Run `./vision3 doctor` with the version you have now and look at the
**Access control** section. Everything v0.9.7 listed there as a **WARN** is
a **FAIL** in v0.9.8, and a string with a failure denies every caller,
sysops included. Until now a bad term was just false, so `H25|S50` passed
for level 50 and `!Q` passed for everyone; both now fail for everyone. Strings that read cleanly mean what they did. See
[Checking your strings](reference/acs.md#checking-your-strings).

Two further cases fail the whole string, and doctor warns about both:

- **`L` or `A` checked with no session.** That is only a message area's
  `acs_write` when a reply is uploaded through the QWK API.
- **`AGE` for an account with no sign-up date.**

### 2. V3Net: every board has to sign

This release drops the settings that let a network run with unsigned
traffic. Messages, subscribes and hub replies must all be signed, which
every release since v0.9.6 does.

- **Your board on someone else's hub:** the hub must run v0.9.6 or later.
  A hub that publishes no key has its replies refused, and nothing syncs
  until it upgrades.
- **Your hub:** a leaf older than v0.9.6 can no longer post, and one older
  than v0.9.5 can no longer subscribe. Its sysop upgrades, keeping the node
  key, and carries on.
- `requireSignedMessages`, `requireSignedSubscribe`, `requireSignedHub`,
  `hubSigned` and `hubSignedClears` are gone from the editor. A
  `v3net.json` that still has them loads, and they are ignored.
- Unsigned messages a hub stored before its nodes upgraded stay in your
  message bases. A board that subscribes from now on, or resets its poll
  position, drops them as it meets them.

Upgrade the hub first if you run one: the new Central features below are
offered only where the hub serves them. See
[Signed messages](v3net/configuration.md#signed-messages) and
[Signed subscribes](v3net/configuration.md#signed-subscribes).

### 3. Bring the menu set up to date

A **repo-in-place** install gets these from `git pull`. An **instance** or
**bundle** install does not update `menus/`; copy these from the repo's
`menus/v3/` or the extracted bundle, skipping any you have customised in
`menus.d/`:

| Files | Why |
| ----- | --- |
| `ansi/EMAILM.ANS`, `cfg/EMAILM.CFG` | New **D** (delete current mail), **C** (copy mail to a user) and **V** (view outgoing mail), and a hidden **W** (forward) |
| `ansi/FILEM.ANS`, `cfg/FILEM.CFG` | New **E** (edit an upload) for callers, bound after the sysop's `EDITFILERECORD`, and **R** (remove from batch) |
| `ansi/MSGMENU.ANS`, `cfg/MSGMENU.CFG` | New **X** (Quick Scan), running `NEWSCAN NOW` |
| `ansi/MAIN.ANS`, `cfg/MAIN.CFG` | The status line gains `Net: @UN@`, callers online on other V3Net boards; **W** now runs `LASTCALLERS 20 --all` |
| `ansi/V3SHARE.ANS` | New header art for the V3Net Sharing screen; without it the screen shows a plain title |
| `templates/LASTNET.TOP`, `.MID`, `.BOT` and `templates/WHONET.TOP`, `.MID`, `.BOT` | Layouts for the network Last Callers and Who's Online screens; without them a built-in layout is used |
| `theme.json` | New `toastFadeColors` (default `8,7,15`); without it the fade uses that ramp anyway |

If you have your own menus, the new commands are `DELETEPRIVMAIL`,
`COPYPRIVMAIL`, `FORWARDPRIVMAIL`, `SENTPRIVMAIL`, `EDITUPLOAD`,
`REMOVEFROMBATCH`, `NEWSCAN NOW`, `V3NETCENTRALWHO` and
`V3NETCENTRALLASTCALLERS`. See
[Menu Commands](reference/menu-commands.md).

### 4. Optional: settings and files a new install gets

None of these is needed. Each is something a fresh install has and an
upgraded board does not, until you add it.

- **Last callers at logon.** The stock `login.json` now runs `LASTCALLS`
  with `"data": "20 --all"`, which merges network callers into the list
  once a network shares them. Add the `data` line to your own step if you
  want it. With no network sharing Last Callers the screen is the local one.
- **Viewer doors.** `V` in the file lists can open a full-screen text
  viewer and a ZIP viewer that shows art with its SAUCE titles. To get
  them, copy `scripts/examples/artview.js` and `textview.js`, add the
  `ARTVIEW` and `TEXTVIEW` doors from `templates/configs/doors.json` to
  your `doors.json`, and set `archiveViewerDoor` and `textViewerDoor` in
  `config.json`. Left unset, the built-in viewers run as before. See
  [Viewer doors](files/file-areas.md#viewer-doors).
- **`configs/roles.json`.** Re-running `./setup.sh` copies the template,
  which defines a `validator` role assigned to nobody. With no file there
  are no roles and nothing changes. See [Admin Roles](users/admin-roles.md).
- **`fileMorePrompt`.** The shipped text now lists `C=Nonstop`. It is not
  switched for you; **C** works at the prompt either way.

### 5. Check your SSH host key

The repository carried an SSH private key in `internal/config/` from its
first commit until this release. Nothing read it, and setup has always
generated a fresh key, but check that yours is not a copy:

```bash
ssh-keygen -lf configs/ssh_host_rsa_key
```

If the fingerprint is `SHA256:GRADSvmepqyYvWmRSz7RrsiVudun7o2q0aI1UYnsE8o`,
delete the key and run `./setup.sh` to generate a new one: anyone with the
repository could impersonate your board.

### What else changed

- **V3Net Central grows.** Who's Online and Last Callers across the
  network, activity alerts, and a coordinator-signed board roster join the
  directory. On your existing subscriptions every new switch is off, and a
  caller is shown only after choosing to be. A subscription you add from
  now on starts with **Central**, **Who's Online**, **Last Callers** and
  **Callers Default** on, and **Publish Listing** off; turn off what you do
  not want. See [V3Net Central](v3net/central.md).
- **Network chat needs each caller's agreement.** Lobby Chat stays on for
  every subscription, so a board that chats today keeps chatting. Before a
  caller's first network join, a notice says what leaves the board, and
  **N** keeps them in local chat. Callers from other boards are shown as
  `Eve@Other BBS`. Callers get `/mute`, `/block` and `/ignored`; you get a
  **Net Chat** bar per user in the user editor and a **Chat Block List**
  per subscription.
- **Your board no longer tells its hubs who logs on.** Earlier versions
  sent every caller's handle to each subscribed hub at logon and logoff,
  and nothing used it. Boards on older versions keep sending until they
  upgrade.
- **A board roster hides unsigned chat.** Once a network's coordinator
  publishes a roster, chat lines from boards older than v0.9.8 are hidden
  on upgraded boards. A coordinator should wait for the network to upgrade
  before publishing one. See [Board Roster](v3net/central.md#board-roster).
- **Messages arrive when they are posted.** A leaf now polls as soon as
  the hub announces a new message. The poll interval is the fallback.
- **Admin roles and user groups.** `configs/roles.json` grants
  `users.validate`, `users.edit`, `users.ban`, `messages.moderate` and
  `files.manage` to named roles, the last two per area or conference, and
  defines groups for the `GROUP:` term and for mail to `@group`. Sysop,
  co-sysop and sponsor powers are as they were.
- **A handle can no longer start with `@`,** since `@name` addresses a
  group. Doctor warns when an existing handle matches a group's address.
- **Sponsors and uploads belong to accounts.** At its first start the new
  version binds each area's sponsor and each file's uploader to the
  account holding that handle. A name that matches no account is bound to
  nobody, and doctor warns about it; set the sponsor again to fix it.
- **Purged and renamed handles are retired.** Nobody can register a
  handle a purged account had, or a renamed account's old one, so nobody
  inherits its private mail. The list is `data/users/retired_handles`;
  delete a line to release a handle. User IDs are never reused either
  (`data/users/next_user_id`). Doctor also warns about two existing
  handles that private mail cannot tell apart.
- **Moderation from the reader.** `D` deletes, `E` edits and `V` moves a
  message, for co-sysops everywhere and for an area's sponsor in that
  area. Authors can edit their own posts in local areas, and callers can
  delete their own private mail and unsend a letter nobody has read. See
  [Editing a Posted Message](messages/message-areas.md#editing-a-posted-message)
  and [Moving a Message](messages/message-areas.md#moving-a-message).
- **Dates follow the board's `timezone`.** Message, news, file, rumor and
  other dates were shown in the host's zone. If your `timezone` differs
  from the host's, the dates callers see shift to match the clock
  everything else already used. A newscan date a caller types is midnight
  on the board.
- **Long lists pause.** The user list, file search, file newscan and the
  rumor list now stop at each screen with a More prompt, where **C** runs
  the rest without stopping. Callers can turn the pauses off with **More
  Prompts** in Konfig.
- **Semicolons chain menu commands.** `m;x` at the main menu opens the
  message menu and starts a quick scan. A menu key that contains `;` still
  works when typed alone. See
  [Chaining Commands](menus/menu-system.md#chaining-commands).
- **Wrong SSH passwords count toward the IP lockout.** A wrong password
  for a known handle at the SSH prompt is now a failed login, like one at
  the LOGIN screen, and a locked-out address is not logged in by SSH
  either. SSH, the LOGIN menu and the QWK API check one login per address
  at a time, so callers sharing an address, such as a web-terminal
  gateway, wait for each other. See
  [Authentication Lockout](configuration/security.md#authentication-lockout).
- **Old ZIPs upload.** Archives packed with PKZIP 1.x (shrunk, reduced or
  imploded) were rejected as corrupt; ZipLab and the viewers now read them.
- **Toasts for new posts, and a fade.** `toastPosts` announces a post to
  the other callers online who may read the area, and `toastFade` fades
  overlay toasts in and out. Both are off until you turn them on under
  **System Setup → Toasts**.
- **ACS fields take 255 characters** in `./config` and `menuedit`. They
  stopped at the width of the field, as short as 10.
- **Changed chat strings.** `chatHelp` and `chatMsgUsage` are replaced
  with the new text if you never edited them. If you did, add `/mute`,
  `/block` and `/ignored` to your own help.
