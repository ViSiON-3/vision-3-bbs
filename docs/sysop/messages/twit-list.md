# Twit Filter List

Each caller can hide messages from selected authors. Open **K (User Configuration)**
and choose **M (Twit List)**, or choose **I (Twit List)** from the stock Message Menu.
Custom menus can call `RUN:CFG_TWITLIST`. The current full-screen `USERCONFIG`
replaces the older `USERCFG` menu mentioned in issue #398.

Use A to add a handle or network name, D to delete an entry by number, C to clear
with confirmation, and Q to leave. Changes save immediately in the caller's
`twitList` in `users.json`. They are session preferences, not sysop-owned fields.
In the message reader, **I** confirms adding the displayed author and immediately
moves forward. **T** keeps its existing Thread function. Anonymous authors cannot
be quick-added and anonymous posts are always visible.

An exact local handle is stored by user ID, including handles containing `@`.
Otherwise, `@` separates a network name from its address. Matching uses
that user's current handle, current real name (including RealNameOnly areas), and
the name saved at add time. This survives renames. Other names are network entries.
Use `name@address` to match only that origin; without an address the name matches
on every network and in local areas. Quick-add includes the origin address of
imported network messages, keeping those authors separate from similarly named accounts. Locally written echomail/netmail
posts retain local account IDs and checks even though they also carry an origin address.
Matching ignores case and leading/trailing whitespace. Anonymous means the
configured `anonymousName` or the standard `Anonymous` signature; the board does
not retain a separate anonymous author identity for filtering.

In `config.json`:

| Setting | Default | Meaning |
| --- | --- | --- |
| `twitListMax` | 50 | Maximum entries; zero or negative uses 50. |
| `twitExemptLevel` | sysop level | Local users at or above this level cannot be added. Zero or negative uses `sysOpLevel`. Sysops are always exempt. |

You cannot add yourself. Local exemptions are rechecked when the reader/list or
QWK export builds its matcher; promoted users become visible on the next entry.
Network entries have no account security level and are not exempt, even when their
name matches a sysop. Duplicate entries are refused.

Filtering applies to READMSGS, NEWSCAN, LISTMSGS, READPRIVMAIL, LISTPRIVMAIL,
thread searches, and QWK downloads (terminal and HTTPS API). The API resolves the
latest saved list on each packet request, including requests using an older login token. Privacy and search filters still apply.
Messages are skipped without placeholders. Newscan shows one summary of unique
visited messages hidden by the Twit List, across all scanned areas. Messages
excluded by privacy/search rules are not counted. Repeated navigation does not
inflate the total.

Area Total/New/Personal counts and the newscan area's “X new” count remain raw JAM
counts, so the reader may show fewer messages. QWK export applies the list before
its per-area cap; last-read advances still require a successful packet transfer.
Filtering does not delete messages, notify authors, block mail delivery, or affect
pages, node messages, chat, one-liners, rumors, user lists, or last callers.

Prompts, confirmations, errors, and the scan summary are editable through the
`twit*` entries in `strings.json` and the string editor.
