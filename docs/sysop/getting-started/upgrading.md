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

## Coming from a version older than v0.9.3

The worked example below covers one step, v0.9.3 to v0.9.4. Upgrade notes are
cumulative: coming from further back, work through each release you are
skipping, oldest first, before the one below. The notes for v0.9.0 to v0.9.3
are in the guide as it stood at v0.9.3:

| Upgrading to | What needs doing by hand |
| ------------ | ------------------------ |
| [v0.9.0](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v090) | Replace `bin/binkd`, which was a broken build; add two steps to `login.json`; copy the menu set |
| [v0.9.1](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v091) | Anonymous posting is off by default; new file-menu commands in `FILEM.CFG` |
| [v0.9.2](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v092) | Custom `login.json`: put `SYSOPNOTICES` and `NMAILSCAN` first |
| [v0.9.3](https://github.com/ViSiON-3/vision-3-bbs/blob/v0.9.3/docs/sysop/getting-started/upgrading.md#worked-example-upgrading-to-v093) | Two or more FTN networks: each needed its own binkd outbound, which v0.9.4 now does for you |

Where those notes say to restart after a config edit, see
[Restarting](#restarting) instead: most files have reloaded on save since
v0.9.2.

## Worked example: upgrading to v0.9.4

v0.9.4 is a large release. Deploy the new programs and restart as usual, then
do three things by hand: run `v3mail readdress` once, bring your menu set up
to date, and repoint any custom menu that uses a removed command. The rest of
this section is behaviour that changed and is worth knowing about.

`bin/binkd` and `bin/sexyz` are unchanged. The `wfc` console is one of the
programs to copy: it ships in the release bundle from this version, and
`build.sh` builds it. A new console works against an older BBS, but kick and
the new counters need the BBS updated too. See
[WFC Console](how-to-guides/wfc-console.md).

### 1. Run `v3mail readdress` once

A private message can now be read only by the account whose **handle** is in
its To or From field. Before this release the area's read ACS was all that
protected one, so anyone with access to the area could read other users'
private mail. Real names are no longer trusted for this: they are neither
unique nor fixed.

Mail stored before the upgrade may be addressed by real name or to `Sysop`,
which is how netmail and QWK mail arrive. Until it is readdressed, its
recipient cannot see it. From the BBS directory:

```bash
./v3mail readdress --all --dry-run   # see what would change
./v3mail readdress --all
```

It rewrites To with the recipient's handle wherever the name resolves to one
account (a handle, `Sysop` for user #1, or a real name only one user has), and
reports what it left alone. It is safe to run twice. Mail it cannot resolve is
**undeliverable**: a sysop can read it in the message reader, list and
newscan. Apart from its sender, if the sender is a local user, nobody else
can. New mail is addressed by handle as it is tossed or imported. See [Readdressing private mail](messages/v3mail.md#readdressing-private-mail).

Other things that follow from the same rule:

- There is no sysop bypass for delivered mail. A sysop reads their own mail
  and undeliverable mail, not other users'.
- The mailbox commands (`READPRIVMAIL`, `LISTPRIVMAIL`) show mail addressed
  **to** you. Mail you sent, and undeliverable mail for a sysop, shows in the
  message reader, list and newscan for the area.
- `COMPOSEMSG` requires a recipient in `PRIVMAIL` and in netmail areas, where
  it used to default to "All". Netmail needs an address: enter
  `Name@zone:net/node`, or the name and then the address. Netmail with no
  address used to go to the first link, usually the hub, so writing to the hub
  sysop now means typing the hub's address.
- Scripts: `v3.message.get` returns null for a private message the running
  user cannot see. Outside netmail areas, `v3.message.postPrivate` throws
  unless `to` resolves to an account; in a netmail area `to` is used as given.

### 2. Bring the menu set up to date

A **repo-in-place** install gets the new menu set from `git pull`. Move your
own edits into `menus.d/` first, as described under
[Repo in place](#repo-in-place), or the pull will conflict with them.

An **instance** or **bundle** install does not update `menus/`, and this
release needs it to. Copy these from the repo's `menus/v3/` or the extracted
bundle, skipping any you have customised:

| Files | Why |
| ----- | --- |
| `cfg/MAIN.CFG` | **K** now runs `RUN:USERCONFIG` (see step 3) |
| `cfg/ADMIN.CFG`, `ansi/ADMIN.ANS` | New **M** key, Poll Mail Networks (`RUN:MAILPOLL`) |
| `ansi/KONFIG.ANS` | New: header art for the user Konfig editor |
| `ansi/FASTLOGN.ANS`, `MSGMENU.ANS`, `NEWSCAN.ANS`, `NICETRY.ANS`, `TIMEOUT.ANS` | Redrawn, with explicit row breaks |
| `templates/message_headers/MSGHDR.2.ans` to `MSGHDR.10.ans`, `MSGHDR.12.ans` | Reworked so a long name or subject cannot overflow a row |

These are no longer used and can be deleted: `ansi/ALIAS.ANS`,
`ansi/CONFIG.ANS`, `ansi/CONFIG_F.ANS`, `ansi/SCANSETUP.ANS`,
`ansi/USERCFG.ANS`, `cfg/USERCFG.CFG`, `mnu/USERCFG.MNU`,
`templates/USRCFGV.TOP` and `templates/USRCFGV.BOT`.

Whatever your layout, this is the release to start keeping your own menu files
in `menus.d/`. It is searched before `menus/`, file by file, and no upgrade
touches it. See
[Customising menus without losing your changes](menus/menu-system.md#customising-menus-without-losing-your-changes).
A file you override there keeps its old contents, so an overridden `MAIN.CFG`
or `ADMIN.CFG` needs the edits below made by hand.

### 3. Custom menus: removed commands

The **K** user configuration menu is replaced by a full-screen editor,
`RUN:USERCONFIG`. The old `USERCFG` menu is gone, and so are the commands only
it used:

```text
CFG_SCREENWIDTH   CFG_SCREENHEIGHT  CFG_TERMTYPE     CFG_HOTKEYS
CFG_MOREPROMPTS   CFG_CUSTOMPROMPT  CFG_COLOR        CFG_REALNAME
CFG_NOTE          CFG_FILELISTMODE  CFG_VIEWCONFIG
```

`CFG_AUTOSIG`, `CFG_PASSWORD` and `CFG_FILECOLUMNS` remain. A menu entry that
still uses `GOTO:USERCFG` or one of the removed commands reports that the menu
or command was not found. Point it at the editor instead:

```json
{
    "KEYS": "K",
    "CMD": "RUN:USERCONFIG"
}
```

To put the new poll command on a customised admin menu, add:

```json
{
    "KEYS": "M",
    "CMD": "RUN:MAILPOLL",
    "ACS": "S255",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Polling Mail Networks"
}
```

Keys left in your `strings.json` for the removed commands are ignored.

**Hot keys now work.** The user setting and `FORCEHOTKEY` in a `.MNU` file
were both stored and never read. A user who turned Hot Keys on in the past,
and any menu of yours with `FORCEHOTKEY` set to `true`, will start running
commands on a single keypress after this upgrade. The shipped menus all have
it `false`.

### FTN: outbounds, the dupe database, bad and dupe areas

- **Networks sharing one outbound are split automatically.** v0.9.3 asked you
  to give each network its own **Binkd Outbound**. If several still share the
  global directory, the BBS splits them at startup: one network keeps the
  global directory and each of the others gets `<global>_<network>`, such as
  `data/ftn/out_zeronet`. The new paths are saved to `ftn.json`, the `domain`
  lines in `data/ftn/binkd.conf` are repointed, and the log names each network
  moved. Mail already queued for a network that moved stays in the old
  directory; move that hub's bundle and flow files across by hand. See
  [Adding a Second Network](messages/ftn-echomail.md#adding-a-second-network).
- **The dupe database changed its key** to `<ECHO TAG> <MSGID>`, so a message
  crossposted to several echoes reaches all of them. Entries written by older
  versions no longer match and age out within 30 days. In that window a
  message that arrived before the upgrade and arrives again after it can be
  imported twice.
- **Set up a bad area if you have none.** With no bad area, a message for an
  echo you do not carry fails the toss and its packet is moved to `temp_path`.
  Run the FTN Setup Wizard on an existing network and leave **Bad/Dupe Areas**
  at Y to create `ftn_bad` and `ftn_dupe`, or pick existing areas under
  **Echomail Networks**, **G** (Global). Both settings are now chosen from a
  list of message areas; a tag typed by hand that named no area used to be
  accepted and then ignored. See
  [Bad/undeliverable messages](messages/ftn-echomail.md#badundeliverable-messages).
- **Packets in `temp_path` can be tossed again.** A message that failed to
  toss was already marked as seen, so re-tossing its packet discarded it as a
  dupe. A message is now recorded only once it is stored. Because the old
  entries no longer match, packets an older version left in `temp_path` can
  be moved back to the inbound and tossed once the cause is fixed.
- **Echo tags match ignoring case**, and two areas whose echo tags differ only
  in case are refused as duplicates.
- **`v3mail` exits 1 on failure.** `stats`, `pack`, `purge`, `lastread` and
  `link` used to exit 0 when a base could not be opened, and `fix --repair`
  and `pack` when the pack failed. A maintenance event that has been failing
  quietly will now show as failed in the scheduler.
- **On-demand polling.** `./v3mail poll`, or **M** on the admin menu, sends
  and fetches mail for every FTN and QWK network. See
  [Poll](messages/v3mail.md#poll).

### The config editor's main menu has moved

QWK Networking and ZipLab are new entries, which shifts the keys after
Echomail Networking:

| Key | v0.9.3 | v0.9.4 |
| --- | ------ | ------ |
| 5 | ViSiON/3 Networking (V3Net) | QWK Networking |
| 6 | Door Programs | ViSiON/3 Networking (V3Net) |
| 7 | Transfer Protocols | Door Programs |
| 8 | Archivers | Transfer Protocols |
| 9 | Event Scheduler | Archivers |
| 0 | Login Sequence | Event Scheduler |
| A | | Login Sequence |
| B | | ZipLab Upload Processing |

Keys 1 to 4 are unchanged. See
[Configuration](configuration/configuration.md#main-menu).

### New settings and config files

Nothing here needs adding for the BBS to run. `login.json` has no new steps.

| File | What is new |
| ---- | ----------- |
| `config.json` | `disableVgaPalette` (fallback `false`): set it `true` to stop UTF-8 sessions loading the VGA palette. It is not in the config editor yet. |
| `v3net.json` | `hub.autoApproveAreas`. Unset, it follows `autoApprove`, so an existing hub behaves as before. Set it `false` to have area proposals wait in the Coordinator Panel while nodes still join on their own. |
| `qwknet.json` | New file, written when you add a QWK network. See [QWK Networking](messages/qwk-networking.md). |
| `ziplab.json` | Now editable under **ZipLab Upload Processing**. `archiveTypes` and the per-step `command`, `args` and `timeout` keys (other than the virus scan's) are no longer read and are ignored if present. A relative quarantine path now resolves against the BBS root. See [ZipLab](files/ziplab.md). |
| `strings.json` | Five new strings, all with fallbacks: `v3netNewAreaNotice`, `v3netNewAreaDeclined`, `doorRemoteConnecting`, `doorRemoteConnectFailed`, `doorRemoteDisconnected`. |
| `doors.json` | New door type `rlogin`. See [RLogin doors](how-to-guides/door-rlogin.md). |

`SYSOP` and `COSYSOP` in ACS strings now follow `sysOpLevel` and
`coSysOpLevel` from `config.json` instead of a fixed 255 and 250. Nothing
changes on a board that uses those defaults.

### What callers will notice

- **Art on wide and modern terminals.** Telnet no longer caps the reported
  width at 80 columns, and art drawn for 80 columns is fitted to a wider
  window rather than running together. UTF-8 sessions load the VGA palette
  into the caller's terminal and restore the terminal's own at logoff; a
  connection that drops rather than logs off leaves the palette in place.
- **News at login pages.** Each item clears the screen and can be scrolled;
  ESC skips the rest of the backlog.
- **Messages show as written.** Lines that fit the screen are no longer
  reflowed, so leading spaces the reader used to drop now show. Pipe codes in
  a message body are colour-only: `||` and control codes such as `|CL` are
  shown as typed.
- **Reading no longer rewinds the newscan pointer.** Paging back to an older
  message leaves newer mail marked as read.
- **Last callers** fits the terminal height: 15 rows on a 25-row screen with
  the stock templates, even where a menu asks for more.
- **A blank subject abandons a post**, and Ctrl-C at a prompt asks to abort
  rather than logging the caller off.

### Scripts

- **`v3.fs` rejects absolute paths.** `/foo` used to be read as
  `scripts/data/foo`; use the relative path. Writes create missing parent
  directories.
- **`exit()` cannot be caught** by the script's own `try`/`catch`.
- **Input and output follow the session's encoding.** V3 scripts receive
  typed characters as Unicode strings and `maxLen` counts characters; output
  is UTF-8 on UTF-8 sessions and CP437 otherwise. In Synchronet JS doors,
  `getstr` ignores arrow keys instead of inserting a `\x01` byte.

### V3Net

- **Sysops are asked about new areas at login.** When a hub adds an area to a
  network you are on, each sysop is asked once whether to add it. Areas the
  network already had are not offered, except those added in the last 30 days
  when a node first sees the network.
- **Coordinator transfer is removed.** The hub signs the area list with its
  own key, so the coordinator is always the hub operator. A hub drops the
  leftover transfer table at startup. Area managers can be reassigned from the
  Coordinator Panel instead.

### Sysop tools

- **User editor:** the mass actions are on Shift+F2, F4, F5 and F10 and work
  on xterm-style terminals. On rxvt, PuTTY's default keyboard mode and the
  Linux console those combinations do nothing. See
  [User Editor](users/user-editor.md).
- **Docker:** add the `./menus.d:/vision3/menus.d` and
  `./ziplab:/vision3/ziplab` mounts from the shipped `docker-compose.yml` to
  yours. The image now carries ZipLab's support files, which were missing, and
  copies any you do not have into `ziplab/` at startup. See
  [Docker](getting-started/docker.md).
