# Source Gateway

The source gateway copies web forums and news sites into message areas.
Callers read them like any other area: each new post or story is a message,
and on sites with replies, each reply is filed under the one it answers.
Nothing is posted back to the sites yet, and callers cannot post in the
gateway's areas.

Two kinds of file drive it:

- A **source definition**, `configs/sources/<name>.json`, describes one site:
  how to load its pages, how to read posts out of them, and how the messages
  look. ViSiON/3 ships definitions for Reddit, Hacker News, and NASA GCN. Adding a site
  means writing a definition, with no code change.
- **`configs/sources.json`** lists the **feeds**. A feed points one source at
  one target on the site, such as a subreddit, and files it into one message
  area. A source can have any number of feeds.

`./helper source sync` does the work. The event scheduler runs it every few
minutes.

## Loading pages

A definition says how its pages are loaded:

- `"fetch": "http"` loads them with plain requests, for sites that serve
  their pages to anyone.
- `"fetch": "browser"` loads them through a Chrome you run with remote
  debugging and log into yourself. Use it for sites that need a login, or
  that refuse automated clients. Reddit is one.

Reddit has closed every easy way in. Its API needs an approved developer
application, and it refuses other automated clients:

| Client                                                      | What Reddit returns                    |
| ----------------------------------------------------------- | -------------------------------------- |
| A plain HTTP client                                         | A login wall or a JavaScript challenge |
| A Chrome that a program starts by itself                    | "Prove your humanity"                  |
| A login cookie copied into any other program                | 403 "Blocked"                          |
| A Chrome that a person started and logged into, then shared | Normal pages                           |

So browser sources use the last one. `helper` attaches to your Chrome, opens
one tab in the background, loads the pages and closes the tab. It keeps only
the part of each page the definition names in `content`; the rest carries
your account name and session token, which the gateway never stores.

Reading Reddit this way is outside Reddit's terms of use. Keep it gentle: a
few subreddits, `page_delay_seconds` of 20 or more, and a schedule of every
15 minutes or longer. If Reddit objects, it may block the account or the
Chrome machine's IP address. The same goes for any site you add.

## Setting up the Chrome machine

Only browser sources need this. The Chrome can run on any machine with a
screen and a desktop session, including one other than the BBS.

1. Start Chrome with a profile used for nothing else. Chrome 136 and later
   refuse remote debugging on your everyday profile.

   ```bash
   # macOS
   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
     --remote-debugging-port=9222 --user-data-dir="$HOME/.reddit-chrome"

   # Linux
   google-chrome --remote-debugging-port=9222 --user-data-dir="$HOME/.reddit-chrome"
   ```

2. In the window that opens, log into the sites your sources read, such as
   <https://old.reddit.com>. If a site asks you to prove you are human, do it
   there.
3. Leave that Chrome running. If it restarts, start it with the same command;
   the profile keeps you logged in.

The debugging port has no password: anything that can reach it controls that
Chrome, including your logins. Chrome only listens on the machine's own
loopback address (`127.0.0.1`). Do not change that, and do not open port 9222
in a firewall.

## Connecting the BBS to it

If Chrome runs on the BBS machine itself, skip this section.

Otherwise, carry the port over SSH from the BBS machine:

```bash
ssh -N -L 9222:127.0.0.1:9222 you@chrome-machine
```

Keep the tunnel up with `autossh`, or a launchd or systemd service that
restarts it. The BBS then reaches Chrome at `http://127.0.0.1:9222`, the
default `chrome_debug_url`. A private network such as a tailnet works too:
set `chrome_debug_url` to the Chrome machine's address on it, and forward
only port 9222 to Chrome's loopback.

## Setting up a feed

1. **Create a message area per feed.** Use a local area, and set its write
   ACS to `S256`, a level above the 255 maximum, so no caller can post:
   replies would never reach the site. `!*` does not work: `*` is only
   special as the whole ACS string, so `!*` lets everyone post.

2. **List the feed** in `configs/sources.json`. New installs get it from
   setup; on an existing board, copy `templates/configs/sources.json` into
   `configs/`:

   ```json
   {
     "chrome_debug_url": "http://127.0.0.1:9222",
     "page_delay_seconds": 20,
     "max_items_per_sync": 25,
     "max_run_seconds": 540,
     "page_timeout_seconds": 60,
     "feeds": [
       {"source": "reddit", "target": "bbs",  "area_tag": "REDDIT_BBS",  "enabled": true},
       {"source": "reddit", "target": "news", "area_tag": "REDDIT_NEWS", "enabled": true, "max_replies": 10}
     ]
   }
   ```

   | Key                     | Default                 | Meaning                                                   |
   | ----------------------- | ----------------------- | --------------------------------------------------------- |
   | `chrome_debug_url`      | `http://127.0.0.1:9222` | Where the logged-in Chrome is reached                     |
   | `page_delay_seconds`    | `20`                    | Pause between page loads                                  |
   | `max_items_per_sync`    | `25`                    | Newest items looked at per feed per run                   |
   | `max_run_seconds`       | `540`                   | Longest a run may take; keep it under the event's timeout |
   | `page_timeout_seconds`  | `60`                    | Longest one page load may take                            |
   | `feeds[].source`        | —                       | Definition name: `reddit` reads `configs/sources/reddit.json` |
   | `feeds[].target`        | —                       | What to read on the site; for Reddit, a subreddit without `r/` |
   | `feeds[].area_tag`      | —                       | Message area to file it into; it must exist               |
   | `feeds[].enabled`       | —                       | `false` skips it                                          |
   | `feeds[].replies`       | `true`                  | `false` imports items and none of their replies           |
   | `feeds[].max_replies`   | none                    | Keep only each thread's first N replies to the item itself, without their replies |

   A busy subreddit such as r/news has threads with thousands of comments.
   Give it `max_replies` (10 is plenty) so each thread brings its top
   comments. A capped thread is loaded again only while it has fewer replies
   than the cap, so busy threads don't reload every run.

3. **Try it** without writing anything:

   ```bash
   ./helper source sync --source reddit --feed bbs --dry-run
   ```

   It lists the items and replies it would import. Then run it for real:

   ```bash
   ./helper source sync --source reddit --feed bbs
   # r/bbs -> REDDIT_BBS: 25 posts, 40 comments, 18 pages
   ```

   With no options, `source sync` runs every enabled feed. `--feed` runs a
   feed even when it is disabled.

4. **Schedule it.** New installs' `configs/events.json` has a `source_sync`
   event, disabled; enable it in `./config` under Event Scheduler. On an
   existing board, add the event there: command `{BBS_ROOT}/helper`,
   arguments `source sync`, schedule `*/15 * * * *`, timeout 600 seconds.

A run never starts a page load that could carry it past `max_run_seconds`.
Each page costs up to `page_delay_seconds` plus `page_timeout_seconds`, so a
first sync of 25 posts takes several runs; each stops cleanly, and the next
continues where it left off. Keep `max_run_seconds` below the event's
`timeout_seconds` (600): a run the scheduler kills leaves a tab open in the
Chrome.

The first run imports a feed's newest items (up to `max_items_per_sync`)
with their replies. Later runs import only new items, and reload an item's
thread page only when its change marker has moved; for Reddit the marker is
the comment count.

## DEEP SPACE: NASA GCN Circulars

The `gcn` definition imports public observing-team bulletins from NASA's
[General Coordinates Network](https://gcn.nasa.gov/docs/circulars). These
are human-authored reports of detections, candidates, follow-up observations,
and corrections. They are separate from the real-time machine Notice stream,
which uses authenticated streaming subscriptions.

Create a local read-only `DEEP_SPACE` area (`acs_write: "S256"`), install
`templates/configs/sources/gcn.json` in `configs/sources/`, and append:

```json
{"source": "gcn", "target": "circulars", "area_tag": "DEEP_SPACE", "enabled": true, "replies": false}
```

The public HTML index supplies stable Circular IDs and subjects. Each new
bulletin's public JSON download supplies its original author, UTC publication
time, and text. Formatting and scientific qualifiers are preserved; the
gateway does not infer classifications, confidence, or sky coordinates.
The canonical GCN page is linked at the end. New follow-up/correction Circulars
arrive as separate messages; in-place edits to an already imported Circular
are not copied back into the BBS. The source link always opens NASA's latest
version.

No NASA account, API key, or Chrome is needed. The definition paces new
detail requests one second apart without changing Reddit's existing delay.
Preview with `helper source sync --source gcn --dry-run`. Schedule
`helper source sync --source gcn` every 15 minutes with a timeout of 600
seconds, above the normal 540-second run budget.

## Hacker News headlines

The `hackernews` source reads external-link headlines directly over HTTP;
Chrome and a site login are not needed. Each message contains the headline
and article link, with a stable Hacker News story ID for deduplication.
It does not copy articles, comments, or HN text-only posts. Messages are
dated when imported and signed `Hacker News`, rather than a submitter name.

Create a read-only local area such as `HACKER_NEWS` (`acs_write: "S256"`),
then append this feed to `sources.json`:

```json
{"source": "hackernews", "target": "news", "area_tag": "HACKER_NEWS", "enabled": true, "replies": false}
```

Targets are `news` (the front page), `newest`, and `best`. On older binaries,
copy `templates/configs/sources/hackernews.json` into `configs/sources/` so
the definition is available without rebuilding helper. A board still using
`reddit.json` must first migrate its existing settings and subreddit feeds
into `sources.json` as described below; otherwise the Reddit feeds would
disappear from the active configuration.

Preview with `./helper source sync --source hackernews --dry-run`, then
remove `--dry-run` to import. A separate scheduled event with arguments
`["source", "sync", "--source", "hackernews"]` can run every 15 minutes.
The normal 25-item limit costs one public listing request per run.

## Boards that used the Reddit gateway

Boards set up before the source gateway have `configs/reddit.json` and no
`sources.json`. They keep syncing with no change: while there is no
`sources.json`, `helper` reads `reddit.json`, and each subreddit becomes a
`reddit` feed, with `comments` and `max_comments` read as `replies` and
`max_replies`. `max_posts_per_sync` is read as `max_items_per_sync`. The setup
scripts and the Docker entrypoint don't add the template `sources.json` to a
board that has `reddit.json`.

`helper reddit sync [--sub <name>]` and `helper reddit dump --url <url>`
still work, so an existing `reddit_sync` event needs no change. Messages
imported before the move keep their MSGIDs, and nothing comes in twice.
`data/reddit/reddit.db` is read once, when `data/source/source.db` is
created, so threads already seen are not loaded again.

To move to `sources.json`, write it with your subreddits as feeds, then
delete `reddit.json`. `configs/reddit_selectors.json` is no longer read; if
you changed it, make the same change in `configs/sources/reddit.json`.

## What the messages look like

For Reddit:

| Field   | Post                              | Comment                     |
| ------- | --------------------------------- | --------------------------- |
| From    | `u/<author>`                      | `u/<author>`                |
| To      | `All`                             | `All`                       |
| Subject | The post title                    | `Re: <post title>`          |
| Date    | When it was posted on Reddit      | When it was posted          |
| Body    | The post's text, or its link      | The comment's text          |

Every message ends with the post's Reddit link, and carries a `REDDIT:`
kludge with the same link. A removed comment arrives as `u/[deleted]`, and
replies to it stay threaded under it. A reply whose parent isn't on the page
is threaded under the post. Edits and deletions made on the site after a
message was imported are not followed.

Comments hidden behind "load more comments" on very long threads are not
imported.

## Writing a definition

A definition is a JSON file in `configs/sources/`, named after the source:
`configs/sources/<name>.json` with `"name": "<name>"` inside. A file in
`configs/sources/` overrides a shipped definition of the same name; to change
the Reddit one, copy `templates/configs/sources/reddit.json` into
`configs/sources/` and edit the copy. `helper` rejects a definition with an
unknown key or a selector that doesn't parse, and names the file and the
problem.

Start from what a page looks like. `source dump` prints the part of a page a
definition keeps:

```bash
./helper source dump --source reddit --url https://old.reddit.com/r/bbs/new/ > listing.html
```

The Reddit definition, shortened:

```json
{
  "name": "reddit",
  "fetch": "browser",
  "threads": true,
  "listing_url": "https://old.reddit.com/r/{feed}/new/",
  "target_pattern": "^[A-Za-z0-9_]{2,21}$",
  "page_origin": "https://old.reddit.com",
  "ready": "div.content[role=\"main\"] div.thing[data-fullname^=\"t3_\"]",
  "content": "div.content[role=\"main\"]",
  "listing": {
    "item": "div.thing[data-type=\"link\"]",
    "exclude": ".child, [data-type]",
    "id_pattern": "^t3_",
    "fields": {
      "id":        {"attr": "data-fullname"},
      "promoted":  {"attr": "data-promoted", "skip_if": "true"},
      "author":    {"attr": "data-author", "default": "[deleted]"},
      "title":     {"sel": "a.title", "text": true},
      "posted_at": {"attr": "data-timestamp", "time": "unix_ms"},
      "url":       {"attr": "data-permalink", "url": true, "origin": "https://www.reddit.com"},
      "thread":    {"attr": "data-permalink", "url": true},
      "marker":    {"attr": "data-comments-count"}
    }
  },
  "thread": {
    "root": "div.thing[data-type=\"link\"]",
    "root_fields": {"id": {"attr": "data-fullname"}, "body": {"sel": ".md", "html": true}},
    "item": "div.thing[data-type=\"comment\"]",
    "exclude": ".child, [data-type]",
    "id_pattern": "^t1_",
    "parent": "nearest_item",
    "fields": {
      "id":        {"attr": "data-fullname"},
      "author":    {"attr": "data-author", "default": "[deleted]"},
      "body":      {"sel": ".md", "html": true},
      "posted_at": {"sel": "time", "attr": "datetime", "time": "rfc3339"}
    }
  },
  "message": {
    "from": "u/{author}",
    "reply_subject": "Re: {root_title}",
    "msgid_suffix": "reddit",
    "kludge": "REDDIT",
    "footer": true,
    "body_fallback": "link"
  }
}
```

**The page.**

| Key              | Meaning |
| ---------------- | ------- |
| `fetch`          | `browser` or `http` |
| `page_delay_seconds` | Optional per-source delay, 0–300 seconds; omitted uses `sources.json` |
| `threads`        | `true` when items have replies on a page of their own; then `thread` is required |
| `listing_url`    | The listing page; `{feed}` is replaced by the feed's target |
| `target_pattern` | Optional regular expression a feed's target must match |
| `page_origin`    | Base for relative links |
| `ready`          | CSS selector to wait for before reading a page (browser sources) |
| `content`        | CSS selector of the part of the page kept; everything outside it is dropped |

**Reading items.** `listing` reads the listing page and `thread` reads a
thread page. Each has:

| Key           | Meaning |
| ------------- | ------- |
| `item`        | CSS selector matching one element per item |
| `exclude`     | Fields never look inside these, such as nested replies |
| `id_pattern`  | Regular expression an ID must match; other items, such as adverts, are skipped |
| `fields`      | How to read each field of an item (below) |
| `root`, `root_fields` | Thread pages only: the thread's own item, and the fields read from it |
| `parent`      | Thread pages only: `nearest_item` makes each reply a reply to the nearest item around it |
| `script`      | Instead of `item` and `fields`: JavaScript run in the page (below) |

Each field reads one value from the item element:

| Key       | Meaning |
| --------- | ------- |
| `sel`     | Read from the first matching element inside the item, outside `exclude`; without it, from the item itself |
| `attr`    | Read this attribute |
| `text`    | Read the element's text |
| `html`    | Read the element's inner HTML (for bodies) |
| `time`    | Parse the value as `unix_ms` or `rfc3339` |
| `url`     | Resolve a relative link against `origin`, or `page_origin` |
| `skip_if` | Skip the item when the value equals this |
| `default` | Use this when the value is empty |
| `suffix` | Append to a nonempty value, e.g. `.json` on a detail URL |

An HTTP source can read a single JSON detail object with
`"thread": {"format": "json", "root_fields": {...}}`. Its fields use JSON
Pointers (`path`, e.g. `/body` or `/createdOn`) in place of CSS selectors.
`required: true` rejects missing/empty values; required timestamps must parse.
IDs must match the HTML listing's item ID before a message can be written.
Plain body strings preserve literal markup, whitespace, and tables; set
`html: true` only when the JSON field actually contains HTML. Missing listing
author/date fields are filled from the detail object. JSON detail extraction
does not fetch replies and is not available to browser sources.

The fields an item can have are `id` (required, stable on the site),
`parent_id`, `title`, `author`, `posted_at`, `body`, `url` (the link shown in
the message), `thread` (the page with its replies), `link` (where a link post
points) and `marker` (something that changes when the replies change, such
as a reply count).

**Scripts.** When selectors can't reach the data, give `script` instead of
`item` and `fields` (browser sources only). It is the body of an async
JavaScript function run in the loaded page, and returns an array of items with
the field names above, `posted_at` as an RFC 3339 string. A thread script
returns the thread's own item first, then the replies; a reply without
`parent_id` is a reply to the thread's item.

```json
"listing": {
  "script": "return [...document.querySelectorAll('article')].map(a => ({id: a.id, title: a.querySelector('h2').textContent, url: a.querySelector('a').href, posted_at: a.dataset.published}))"
}
```

**The messages.**

| Key             | Meaning |
| --------------- | ------- |
| `from`          | The From line; `{author}` is the item's author |
| `reply_subject` | Subject of replies; `{root_title}` is the thread's title |
| `msgid_suffix`  | Required; MSGIDs are `<id>@<suffix>`, which is how nothing comes in twice. Never change it once a feed has synced |
| `kludge`        | Each message carries `<kludge>: <url>` |
| `footer`        | `true` ends each message with the item's link |
| `body_fallback` | Field used as the body when the item has no text, such as `link` |

`{feed}`, the feed's target, works in `from` and `reply_subject` too.

## When it stops working

| Symptom                                          | Likely cause                                   | Fix                                                            |
| ------------------------------------------------ | ---------------------------------------------- | -------------------------------------------------------------- |
| `attaching to Chrome … is the SSH tunnel up?`    | Chrome is closed, or the tunnel is down        | Start Chrome again; check the tunnel                           |
| A load times out                                 | Chrome is logged out, or the site is challenging | Look at the Chrome window; log in or clear the check by hand |
| `0 posts` every run on an active feed            | The site changed its page markup               | Dump a page and update the definition's selectors              |

## Files

| File                            | What it holds                                                                 |
| ------------------------------- | ----------------------------------------------------------------------------- |
| `configs/sources.json`          | Chrome address, timing and the feeds                                          |
| `configs/sources/<name>.json`   | Optional; a definition, or your copy of a shipped one                         |
| `configs/reddit.json`           | Read in place of `sources.json` on boards that have no `sources.json`         |
| `data/source/source.db`         | SQLite: each thread's last-seen marker (`markers`) and a log of runs (`syncs`) |

To see recent runs:

```bash
sqlite3 data/source/source.db 'SELECT source, feed, datetime(finished_at,"unixepoch"), items, replies, pages, error FROM syncs ORDER BY id DESC LIMIT 5'
```

`source.db` is safe to delete. The next run reloads every thread page but
imports nothing twice: the message area's own MSGIDs decide what is already
there.
