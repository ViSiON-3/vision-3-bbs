# Menu Commands Reference

Every command a menu entry or a login step can run, in one place. Each row gives the command name, what goes in its data field, what the user sees, and any access check the code enforces on top of the menu's ACS.

Feature pages (news, rumors, doors, QWK and so on) explain the features. This page only answers "what can I put in a `CMD`, and what does it accept?"

## How commands are written

### In a menu `.CFG` file

```json
{ "KEYS": "LC", "CMD": "RUN:LASTCALLERS 20", "ACS": "*" }
```

| Command form | What it does |
| --- | --- |
| `"CMD": "GOTO:MAIN"` | Jump to another menu |
| `"CMD": "RUN:NAME"` or `"CMD": "RUN:NAME data"` | Run a built-in command from the tables below |
| `"CMD": "DOOR:CODE"` | Launch the door with that code (see [Door Programs](doors/doors.md)) |
| `"CMD": "LOGOFF"` | Disconnect |

The text after `RUN:` is split at the **first space**. The part before it is the command name and is upper-cased. Everything after it is passed to the command as its data, exactly as typed: not trimmed, case preserved, no quoting. Most commands ignore data. The **Data** column below says `none` when a command ignores it.

The menu entry's own `ACS` string is checked by the menu system before the command runs. The **Access** column lists only the checks the command itself makes after that.

### In `login.json`

Login steps use a separate list of step names and take their data from the `data` field:

```json
{ "command": "DISPLAYFILE", "data": "BULLETIN.ANS", "clear_screen": true }
```

See [Login sequence steps](#login-sequence-steps) at the end of this page, and [Login Sequence](users/login-sequence.md) for the step fields.

### Access terms used below

| Term | Meaning |
| --- | --- |
| Logged in | Refuses or does nothing when no user is logged in. Almost every command needs this, so it is only listed when a command is unusual. |
| SysOp | The command checks access level at or above `sysOpLevel` from `config.json` (default 255) |
| CoSysOp+ | The command checks access level at or above `coSysOpLevel` from `config.json` (default 250) |

## Session and login

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `AUTHENTICATE` | none | Login prompt on `LOGIN.ANS`. Returns the authenticated user to the menu system. Refuses if someone is already logged in. | Pre-login only |
| `NEWUSER` | none | New user application. The stock matrix screen uses the bare `NEWUSER` action instead of `RUN:NEWUSER`. | Honours `allowNewUsers` |
| `FULL_LOGIN_SEQUENCE` | none | Runs every step in `login.json` in order, then jumps to `MAIN`. Offers newly flagged newscan areas first. | Logged in |
| `FASTLOGIN` | none | Shows the inline fast-login menu (`FASTLOGN.ANS`, `.BAR`, `.MNU`, `.CFG`). Entries in that CFG decide whether to continue the login, jump to a menu, or log off. | Per-entry ACS in `FASTLOGN.CFG` |
| `MAINLOGOFF` | none | Asks to confirm, then logs off like `IMMEDIATELOGOFF`. | |
| `IMMEDIATELOGOFF` | none | Shows `GOODBYE.ANS` and disconnects with no confirmation. | |
| `PLACEHOLDER` | none | Prints the "undefined option" response. Use it as a stand-in for a key you have not wired yet. | |
| `READMAIL` | none | Placeholder that prints a "read mail" notice and returns. Not a real mail reader. Use `READPRIVMAIL`. | |

## System information

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `SHOWSTATS` | none | The caller's statistics on `YOURSTAT.ANS`, then a pause. Same screen as the `USERSTATS` login step. | |
| `SYSTEMSTATS` | none | System totals from the `SYSSTATS.TOP` and `.BOT` templates. | |
| `LASTCALLERS` | Row limit, whole number greater than 0. Default 20. | Recent callers from the `LASTCALL` templates. Non-numeric data falls back to 20. Hidden logins never use a row. | No login check |
| `SHOWVERSION` | none | Clears the screen and prints the version string. | |
| `LISTUSERS` | none | Public user list from the `USERLIST` templates, alphabetical. | No login check |
| `WHOISONLINE` | none | Who is online, from the `WHOONLN` templates. Invisible nodes are shown only to CoSysOp+. | |
| `ONELINER` | none | Shows the last ten one-liners, then offers to add one. | No login check |

## Chat and paging

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `CHAT` | none | Multi-node and inter-BBS chat. Picks a network, then a room, then enters full-screen chat. | |
| `PAGE` | none | Lists online nodes, asks for a node number and a message, and delivers it to that node. Invisible nodes appear offline unless CoSysOp+. | |

## Message areas and reading

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `LISTMSGAR` | none | Lists the message areas in the current conference from the `MSGAREA` templates, then pauses. | Works logged out (shows all conferences) |
| `SELECTMSGAREA` | none | Lightbar area picker. Left and right switch conference, Enter joins. Falls back to a text prompt if the `MSGAREA` templates are missing. | Conference and area ACS |
| `CHANGEMSGCONF` | none | Lightbar conference picker from the `MSGCONF` templates. Joining also selects the first area in it. | Conference ACS |
| `NEXTMSGAREA` | none | Next readable area in the current conference, wrapping. | |
| `PREVMSGAREA` | none | Previous readable area in the current conference, wrapping. | |
| `NEXTMSGCONF` | none | Next accessible conference, selecting its first area. | |
| `PREVMSGCONF` | none | Previous accessible conference, selecting its first area. | |
| `READMSGS` | none | Opens the message reader in the current area at the first unread message. Prompts for a message number if there is nothing new. Runs `GETHEADERTYPE` first if the user has no header style. | Needs a current area |
| `LISTMSGS` | none | Paged header list for the current area. Enter on a row opens the reader. | Needs a current area |
| `COMPOSEMSG` | Area tag, optional. Blank posts to the current area. | Posts a new message. Prompts for title and recipient, then opens the editor. An unknown tag prints an error and returns. | Area write ACS |
| `PROMPTANDCOMPOSEMESSAGE` | none | Lists areas, asks for an area tag or number, then posts as `COMPOSEMSG` would. Not used in the stock menus. | Area write ACS |
| `NEWSCAN` | `CURRENT` to preset the scope to the current area. Anything else presets tagged areas. | Newscan setup screen (`NSCANHDR.ANS`), then reads new messages. The user can still change the scope on the setup screen. | Area read ACS |
| `NEWSCANCONFIG` | none | Tag and untag areas for the personal newscan. The same tag set drives QWK downloads. | |
| `UPDATENEWSCAN` | none | Moves last-read pointers. Prompts for a date, `A` for everything new or `N` for everything read, then asks for all conferences or the current one. | Area read ACS |
| `GETHEADERTYPE` | none | Lightbar picker for the message header style, from `MSGHDR.BAR` and the files in `templates/message_headers/`. | Silent when logged out |

## Private mail

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `SENDPRIVMAIL` | none | Asks for a recipient handle and subject, then opens the editor. Posts to the `PRIVMAIL` area as a private message. | Needs a `PRIVMAIL` area |
| `READPRIVMAIL` | none | Reads mail addressed to the caller in the `PRIVMAIL` area. | Needs a `PRIVMAIL` area |
| `LISTPRIVMAIL` | none | Header list of the caller's private mail. | Needs a `PRIVMAIL` area |
| `NMAILSCAN` | none | Counts unread private mail and offers to read it now. Silent when mail is not configured. Shipped as a login step, not bound in any stock menu. | |

## QWK offline mail

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `QWKDOWNLOAD` | none | Builds a QWK packet from new messages in the tagged areas, or all readable areas when none are tagged, and sends it. Pointers advance only after a successful transfer. | Area read ACS |
| `QWKUPLOAD` | none | Receives a `.REP` packet and posts the replies in it. | Area write ACS |

## File areas and transfers

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `LISTFILEAR` | none | Lists file areas from the `FILEAREA` templates, then pauses. Not bound in the stock menus. | Area ACS |
| `SELECTFILEAREA` | none | Area picker, lightbar or classic depending on the user's file listing mode. Classic accepts an area number or tag, `?` to list, `Q` to quit. | Area ACS |
| `CHANGEFILECONF` | none | Lightbar conference picker from the `FILECONF` templates. Joining changes the conference for both files and messages. | Conference ACS |
| `NEXTFILEAREA` | none | Next listable area in the current conference, wrapping. | |
| `PREVFILEAREA` | none | Previous listable area in the current conference, wrapping. | |
| `NEXTFILECONF` | none | Next accessible conference, for both file and message menus. | |
| `PREVFILECONF` | none | Previous accessible conference, for both file and message menus. | |
| `LISTFILES` | `EXTENDED` to show every column. Any other word is ignored. | Lists the current area, paged or lightbar depending on the user's file listing mode. | Area list ACS |
| `LISTFILES_EXTENDED` | none | Same as `LISTFILES EXTENDED`. | Area list ACS |
| `VIEW_FILE` | none | Asks for a filename in the current area. Archives open in the contents viewer, other files are paged as text. | Needs a current area |
| `TYPE_TEXT_FILE` | none | Asks for a filename in the current area and pages it as plain text. | Needs a current area |
| `SHOWFILEINFO` | none | Asks for a filename in the current area and prints its full record. | Needs a current area |
| `SEARCH_FILES` | none | Asks for a search string of at least three characters and searches names and descriptions across all areas. | Area ACS on results |
| `DOWNLOADFILE` | none | Asks for a filename, adds it to the batch, then starts the transfer. | Area download ACS |
| `BATCHDOWNLOAD` | none | Transfers the tagged batch queue. | Area download ACS |
| `CLEAR_BATCH` | none | Empties the tagged batch queue. | |
| `UPLOADFILE` | none | ZMODEM upload into the current area, then duplicate check and description prompts. | Area upload ACS |
| `FILE_NEWSCAN` | `CURRENT` to scan only the current area. Anything else scans the areas tagged in `FILENEWSCANCONFIG`, or every listable area when nothing is tagged. | Lists files uploaded since the newscan cutoff, grouped by area, from the `FILESCAN` templates. The cutoff is set by `SETFILESCANDATE`, or the previous logon. | Area list ACS |
| `FILENEWSCANCONFIG` | none | Tag and untag file areas for the file newscan. | |
| `SETFILESCANDATE` | none | Sets the file newscan cutoff. Accepts a date as MM/DD/YY, `A` for all files, or `R` to reset to the previous logon. | |
| `WANTLIST` | none | For CoSysOp+, manages the file want list. For everyone else, asks for a filename and reason and adds a request. Stock menus restrict it to sysops. | Branches on CoSysOp+ |
| `EDITFILERECORD` | none | Upload review queue. Asks whether to review all areas or the current one, then edits, moves, or deletes each unreviewed file. | CoSysOp+, silent otherwise |

## User settings

All of these apply to the logged-in user. The stock main menu binds `K` to `USERCONFIG`, which covers every setting below in one screen. The single-setting `CFG_*` commands remain for custom menus.

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `USERCONFIG` | Optional menu name to go to on exit. Without one, the calling menu is redisplayed. | Full-screen settings editor: screen size, encoding, hot keys, message header style, auto-signature, real name, location, note, password, file listing mode and columns. Draws `USERCFG.ANS` (at most 5 rows) as its header. Arrow keys move, Enter changes, a setting's letter jumps straight to it, Q or Esc leaves. Each change is saved as soon as it is confirmed. A new screen size applies at once; a new encoding applies from the next login. | |
| `CFG_VIEWCONFIG` | none | Read-only summary of the user's settings, wrapped in the `USRCFGV` templates. | |
| `CFG_HOTKEYS` | none | Toggles hot keys: menu commands run on a single keypress, falling back to line input when the key could start a longer command. | |
| `CFG_MOREPROMPTS` | none | Toggles more prompts. Stored only: nothing reads it yet. | |
| `CFG_SCREENWIDTH` | none | Prompts for a screen width from 40 to 255. | |
| `CFG_SCREENHEIGHT` | none | Prompts for a screen height from 21 to 60. | |
| `CFG_TERMTYPE` | none | Toggles the saved encoding between CP437 and UTF-8 with no prompt. Applies from the next login. | |
| `CFG_FILELISTMODE` | none | Toggles the file listing mode between classic and lightbar with no prompt. | |
| `CFG_FILECOLUMNS` | none | Toggle screen for the columns shown in file listings. | |
| `CFG_COLOR` | Colour slot number 0 to 6. Anything else means slot 0. | Shows the palette and prompts for a colour for one slot: 0 prompt, 1 input, 2 text, 3 stat, 4 text2, 5 stat2, 6 bar. Stored only: nothing reads the colour slots yet. | |
| `CFG_CUSTOMPROMPT` | none | Prompts for a custom prompt string of up to 80 characters. Stored only: nothing reads it yet. | |
| `CFG_REALNAME` | none | Prompts for a new real name, validated before saving. | |
| `CFG_NOTE` | none | Prompts for the user's private note, up to 35 characters. | |
| `CFG_PASSWORD` | none | Asks for the current password, then a new one. | |
| `CFG_AUTOSIG` | none | Auto-signature editor. Change, delete, or quit. Up to five lines. | |

## Doors

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `DOOR:` | Door code, written as `DOOR:CODE` rather than `RUN:` | Launches a configured door. See [Door Programs](doors/doors.md). | Per-door minimum level, single-instance lock |
| `LISTDOORS` | none | Sorted door list from the `DOORLIST` templates. Doors above the caller's level are hidden. Not bound in the stock menus. | |
| `OPENDOOR` | none | Asks for a door code, `?` to list, `Q` to quit, then launches it. Not bound in the stock menus. | Per-door minimum level |
| `DOORINFO` | none | Asks for a door code and prints its configuration. Not bound in the stock menus. | |
| `RUNDOOR` | Path to a script or program | Runs the program with the session's terminal and the node number as its only argument. Meant for the login sequence. Missing path or file is logged and skipped. | No access check |
| `DISPLAYFILE` | Filename of an ANSI or text file | Displays the file. Meant for the login sequence. A missing file is logged and skipped. | |

## News

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `PRINTNEWS` | none | Shows unseen news items plus any marked Always, then marks them seen. Shipped as a login step. | Per-item level range |
| `LISTNEWS` | none | Lists all visible news items with new markers and lets the user pick items to read. | Per-item level range |
| `EDITNEWS` | none | News manager: add, delete, edit, list, view. | CoSysOp+, silent otherwise |

## Voting and new user voting

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `VOTE` | none | Voting booths: vote, list choices, results, next topic. CoSysOp+ can add and delete topics. | |
| `VOTEMANDATORY` | none | Forces a vote on every mandatory topic the user has not voted on. Meant for the login sequence, not shipped in the default one. | |
| `LISTNUV` | none | Shows the new user voting queue with tallies. CoSysOp+ get an add, remove, and vote loop. Works even when NUV is off. | |
| `SCANNUV` | none | Vote on every pending candidate the caller has not voted on yet. | `useNuv` on and level at or above `nuvUseLevel` |

## BBS list

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `BBSLIST` | none | Split-panel lightbar browser of BBS listings. | |
| `BBSLISTADD` | none | Prompts for name, address, ports and other fields, then adds a listing owned by the caller. | |
| `BBSLISTEDIT` | none | Asks for an entry number and edits it. | Owner or CoSysOp+ |
| `BBSLISTDELETE` | none | Asks for an entry number, confirms, and removes it. Owners cannot delete their own entries. | CoSysOp+ |
| `BBSLISTVERIFY` | none | Toggles the verified flag on an entry. | CoSysOp+ |

## Rumors

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `RUMORSLIST` | none | Table of visible rumors, then a pause. SysOps see the real author behind anonymous rumors. | Per-rumor minimum level |
| `RUMORSADD` | none | Prompts for rumor text, and for anonymity if the caller's level allows it. Hard cap of 999 rumors. | Level 2 or above; anonymity needs `anonymousLevel` |
| `RUMORSDELETE` | none | Asks for a rumor number and deletes it. | Own rumors, or any for SysOp |
| `RUMORSSEARCH` | none | Prompts for text and matches it against rumor text and author. | Per-rumor minimum level |
| `RUMORSNEWSCAN` | none | Rumors posted since the caller's last login. | Per-rumor minimum level |
| `RANDOMRUMOR` | none | Prints one random visible rumor with no pause. Usable as a login step. | |

## InfoForms

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `INFOFORMS` | none | Lists forms 1 to 5 with required and completed status. Quitting is blocked while a required form is incomplete. | Per-form minimum level |
| `INFOFORMVIEW` | none | Asks for a form number and shows the caller's own answers. | |
| `INFOFORMREQUIRED` | none | Forces unvalidated users to complete required forms, and disconnects them if they refuse. Shipped as a login step. Does nothing for validated users. | |
| `INFOFORMHUNT` | none | Asks for a form number and prints every user's answers. | SysOp |
| `INFOFORMNUKE` | none | Asks for a handle, confirms, and deletes all of that user's form answers. | SysOp |

## V3Net

Stock menus bind all of these in `V3NETM` with ACS `S255`. None of them check a level in code.

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `V3NETSTATUS` | none | Node ID, hub mode, subscription count and leaf networks. Prints a disabled notice when V3Net is off. | |
| `V3NETAREAS` | Network name, optional. Blank covers every subscribed network. | Fetches each network's area list and shows a lightbar for subscribing and unsubscribing. | Silent when V3Net is off |
| `V3NETPROPOSE` | Network name, optional. Blank uses the first subscribed network. | Form to propose a new network area. | Silent when V3Net is off |
| `V3NETREGISTRY` | none | Fetches the network registry and lists networks, marking subscribed ones. | |
| `V3NETACCESSREQUESTS` | none | Pending subscription requests for every area this node manages, across all subscribed networks. `A` approves, `D` denies and adds the node to the area's deny list. | Area manager, checked by the hub |
| `V3NETCOORDINATOR` | none | Coordinator panel for networks whose NAL names this node as coordinator. `P` opens the pending proposal queue, where `A` approves a proposal as submitted and `R` rejects it with an optional reason. | Network coordinator, checked by the hub |

## SysOp and administration

| Command | Data | What it does | Access |
| --- | --- | --- | --- |
| `PENDINGVALIDATIONNOTICE` | none | One-line notice that users await validation. Silent when none do. Bound to the `//` auto-run key in the stock main menu. | SysOp |
| `NEWUSERVAL` | none | Counts pending users and offers to review them. Silent when none. Shipped as a login step. | SysOp, checked in code regardless of `sec_level` |
| `SYSOPNOTICES` | none | Shows queued sysop notices such as new user joins. Shipped as the first login step. | CoSysOp+, silent otherwise |
| `VALIDATEUSER` | none | The user editor, filtered to accounts awaiting validation. | SysOp |
| `ADMINLISTUSERS` | none | The full user editor. | SysOp |
| `UNVALIDATEUSER` | none | Lightbar user picker, then removes validation. Not bound in the stock menus. | SysOp |
| `BANUSER` | none | Lightbar user picker, then bans the account. Not bound in the stock menus. | SysOp |
| `DELETEUSER` | none | Lightbar user picker, then soft-deletes the account. Not bound in the stock menus. | SysOp |
| `PURGEUSERS` | none | Permanently removes soft-deleted users older than `deletedUserRetentionDays`. A negative value disables purging. | SysOp |
| `TOGGLEALLOWNEWUSERS` | none | Flips `allowNewUsers` in `config.json` and reports the new state. | SysOp |
| `SPONSORMENU` | none | Sponsor menu for the current message area: edit the area, step through sponsored areas, reorder. | Area sponsor, or CoSysOp+ |
| `SPONSOREDITAREA` | none | Field editor for the current message area. Tag, base path, type, echo and network fields need CoSysOp+. | Area sponsor, or CoSysOp+ |

## Login sequence steps

Steps in `login.json` use these names in the `command` field. Some are aliases for a `RUN:` command above, and four exist only as login steps. Any other name is logged and skipped. A `DOOR:CODE` command also works as a step.

| Step | Data | Runs |
| --- | --- | --- |
| `LASTCALLS` | Row limit, default 20 | Same as `LASTCALLERS` |
| `ONELINERS` | none | Same as `ONELINER` |
| `USERSTATS` | none | Same as `SHOWSTATS` |
| `NMAILSCAN` | none | Same as `NMAILSCAN` |
| `DISPLAYFILE` | Filename | Same as `DISPLAYFILE` |
| `RUNDOOR` | Script path | Same as `RUNDOOR` |
| `FASTLOGIN` | none | Same as `FASTLOGIN` |
| `NEWUSERVAL` | none | Same as `NEWUSERVAL` |
| `WHOISONLINE` | none | Login variant of `WHOISONLINE`. Silent when nobody else is visible, otherwise asks before showing the list. |
| `PRINTNEWS` | none | Same as `PRINTNEWS` |
| `VOTEMANDATORY` | none | Same as `VOTEMANDATORY` |
| `CHECKNUV` | none | Login only. Tells eligible users that NUV candidates await their vote. |
| `SYSOPNOTICES` | none | Same as `SYSOPNOTICES` |
| `RANDOMRUMOR` | none | Same as `RANDOMRUMOR` |
| `INFOFORMREQUIRED` | none | Same as `INFOFORMREQUIRED` |

The shipped `login.json` runs, in order: `SYSOPNOTICES`, `NMAILSCAN`, `FASTLOGIN`, `PRINTNEWS`, `LASTCALLS`, `ONELINERS`, `USERSTATS`, `INFOFORMREQUIRED`, `NEWUSERVAL` at `sec_level` 255, and `CHECKNUV`.
