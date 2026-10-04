# Reddit Gateway

The Reddit gateway copies subreddits into message areas. Each new post starts
a thread and each comment is filed as a reply under the comment it answers, so
callers read a subreddit like any other area. It is read-only: nothing is ever
posted to Reddit, and callers cannot post in the gateway's areas.

`./helper reddit sync` does the work. The event scheduler runs it every few
minutes.

## How it reads Reddit

Reddit has closed every easy way in. Its API needs an approved developer
application, and it refuses other automated clients:

| Client                                                      | What Reddit returns                    |
| ----------------------------------------------------------- | -------------------------------------- |
| A plain HTTP client                                         | A login wall or a JavaScript challenge |
| A Chrome that a program starts by itself                    | "Prove your humanity"                  |
| A login cookie copied into any other program                | 403 "Blocked"                          |
| A Chrome that a person started and logged into, then shared | Normal pages                           |

So the gateway uses the last one. You run Chrome with remote debugging on a
machine of your choice, log into Reddit in it yourself, and leave it running.
`helper` attaches to that Chrome, opens one tab, reads old Reddit
(`old.reddit.com`) pages and closes the tab. It keeps only the part of each
page with the posts and comments; the rest carries your Reddit name and
session token, which the gateway never stores.

Reading Reddit this way is outside Reddit's terms of use. Keep it gentle: a
few subreddits, `page_delay_seconds` of 20 or more, and a schedule of every
15 minutes or longer. If Reddit objects, it may block the account or the
Chrome machine's IP address.

## Setting up the Chrome machine

The Chrome can run on any machine with a screen and a desktop session,
including one other than the BBS.

1. Start Chrome with a profile used for nothing else. Chrome 136 and later
   refuse remote debugging on your everyday profile.

   ```bash
   # macOS
   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
     --remote-debugging-port=9222 --user-data-dir="$HOME/.reddit-chrome"

   # Linux
   google-chrome --remote-debugging-port=9222 --user-data-dir="$HOME/.reddit-chrome"
   ```

2. In the window that opens, log into <https://old.reddit.com>. If Reddit
   asks you to prove you are human, do it there.
3. Leave that Chrome running. If it restarts, start it with the same command;
   the profile keeps you logged in.

The debugging port has no password: anything that can reach it controls that
Chrome, including your Reddit login. Chrome only listens on the machine's own
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
default `chrome_debug_url`.

## Setting up the gateway

1. **Create a message area per subreddit.** Use a local area, and set its
   write ACS to `S256`, a level above the 255 maximum, so no caller can post:
   replies would never reach Reddit. `!*` does not work: `*` is only special
   as the whole ACS string, so `!*` lets everyone post.

2. **Map subreddits to areas** in `configs/reddit.json`. New installs get it
   from setup; on an existing board, copy `templates/configs/reddit.json`
   into `configs/`:

   ```json
   {
     "chrome_debug_url": "http://127.0.0.1:9222",
     "page_delay_seconds": 20,
     "max_posts_per_sync": 25,
     "subreddits": [
       {"subreddit": "bbs", "area_tag": "REDDIT_BBS", "enabled": true}
     ]
   }
   ```

   | Key                     | Default                 | Meaning                                                   |
   | ----------------------- | ----------------------- | --------------------------------------------------------- |
   | `chrome_debug_url`      | `http://127.0.0.1:9222` | Where the logged-in Chrome is reached                     |
   | `page_delay_seconds`    | `20`                    | Pause between page loads                                  |
   | `max_posts_per_sync`    | `25`                    | Newest posts looked at per subreddit per run              |
   | `max_run_seconds`       | `540`                   | Longest a run may take; keep it under the event's timeout |
   | `subreddits[].subreddit`| —                       | Subreddit name, without `r/`                              |
   | `subreddits[].area_tag` | —                       | Message area to file it into; it must exist               |
   | `subreddits[].enabled`  | —                       | `false` skips it                                          |

3. **Try it** without writing anything:

   ```bash
   ./helper reddit sync --sub bbs --dry-run
   ```

   It lists the posts and comments it would import. Then run it for real:

   ```bash
   ./helper reddit sync --sub bbs
   # r/bbs -> REDDIT_BBS: 25 posts, 40 comments, 18 pages
   ```

4. **Schedule it.** New installs' `configs/events.json` has a `reddit_sync`
   event, disabled; enable it in `./config` under Event Scheduler. On an
   existing board, add the event there: command `{BBS_ROOT}/helper`,
   arguments `reddit sync`, schedule `*/15 * * * *`, timeout 600 seconds.

A run never starts a page load that could carry it past `max_run_seconds`.
Each page costs up to `page_delay_seconds` plus a minute, so a first sync of
25 posts takes several runs; each stops cleanly, and the next continues where
it left off. Keep `max_run_seconds` below the event's `timeout_seconds` (600):
a run the scheduler kills leaves a tab open in the Chrome.

The first run imports the subreddit's newest posts (up to
`max_posts_per_sync`) with their comments. Later runs import only new posts,
and reload a post's page only when its comment count has gone up.

## What the messages look like

| Field   | Post                              | Comment                     |
| ------- | --------------------------------- | --------------------------- |
| From    | `u/<author>`                      | `u/<author>`                |
| To      | `All`                             | `All`                       |
| Subject | The post title                    | `Re: <post title>`          |
| Date    | When it was posted on Reddit      | When it was posted          |
| Body    | The post's text, or its link      | The comment's text          |

Every message ends with the post's Reddit link, and carries a `REDDIT:`
kludge with the same link. A removed comment arrives as `u/[deleted]`, and
replies to it stay threaded under it. Edits and deletions made on Reddit after
a message was imported are not followed.

Comments hidden behind "load more comments" on very long threads are not
imported.

## When it stops working

| Symptom                                          | Likely cause                                   | Fix                                                            |
| ------------------------------------------------ | ---------------------------------------------- | -------------------------------------------------------------- |
| `attaching to Chrome … is the SSH tunnel up?`    | Chrome is closed, or the tunnel is down        | Start Chrome again; check the tunnel                           |
| A load times out                                 | Chrome is logged out, or Reddit is challenging | Look at the Chrome window; log in or clear the check by hand   |
| `0 posts` every run on an active subreddit       | Reddit changed its page markup                 | Update the selectors (below)                                   |

**Updating the selectors.** Everything that depends on Reddit's markup lives
in `configs/reddit_selectors.json`: element, class and attribute names. A
board without that file uses the copy in `templates/configs/`; copy it into
`configs/` before editing. To see what a page looks like now:

```bash
./helper reddit dump --url https://old.reddit.com/r/bbs/new/ > listing.html
```

Each post is a `div` with `data-type="link"`; each comment a `div` with
`data-type="comment"`, nested in its parent's `div.child`. Compare the names in
the file with the ones in `reddit_selectors.json` and change what differs.

## Files

| File                            | What it holds                                                                 |
| ------------------------------- | ----------------------------------------------------------------------------- |
| `configs/reddit.json`           | Chrome address, timing and the subreddit-to-area mappings                     |
| `configs/reddit_selectors.json` | Optional; overrides the shipped markup names                                  |
| `data/reddit/reddit.db`         | SQLite: each post's last-seen comment count (`posts`) and a log of runs (`syncs`) |

To see recent runs:

```bash
sqlite3 data/reddit/reddit.db 'SELECT subreddit, datetime(finished_at,"unixepoch"), posts, comments, pages, error FROM syncs ORDER BY id DESC LIMIT 5'
```

`reddit.db` is safe to delete. The next run reloads every post page but
imports nothing twice: the message area's own MSGIDs decide what is already
there.
