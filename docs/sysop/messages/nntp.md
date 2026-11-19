# NNTP text newsgroups

`helper nntp sync` imports articles from a public NNTP reader into local JAM
message areas. Callers read them with the ordinary message reader. It is a
read-only gateway: it never sends POST, IHAVE, or outgoing articles.

## Configure groups

Copy `templates/configs/nntp.json` to `configs/nntp.json`. The template's groups
are disabled. Enable only the groups you want, and create one local message
area per group with `acs_write: "S256"`. The importer rejects writable areas
and networked areas, so articles cannot accidentally leave on FTN/QWK/V3Net.
The areas can share a conference such as Usenet.

The supplied example uses PhreakNet's public text reader, with implicit TLS
on port 563. No registration or Chrome is needed for the selected public
groups. This connector does not support authenticated or private groups.
See the [server's service information](https://phreaknet.org/news) for its
current group coverage and policies. It is a single reader connection,
not a peering feed.

```json
{
  "server": "news.phreaknet.org:563",
  "tls": true,
  "max_articles_per_sync": 25,
  "initial_articles": 25,
  "timeout_seconds": 20,
  "max_run_seconds": 180,
  "max_article_bytes": 262144,
  "groups": [
    {"name": "comp.sys.amiga.misc", "area_tag": "NNTP_AMIGA", "enabled": true},
    {"name": "comp.sys.cbm", "area_tag": "NNTP_CBM", "enabled": true},
    {"name": "alt.bbs", "area_tag": "NNTP_BBS", "enabled": true}
  ]
}
```

TLS verifies the server certificate and is enabled when omitted. Set `tls`
to false only when deliberately using a plain NNTP connection, such as a
local test server. There is no automatic downgrade.

## Preview, import, schedule

```bash
./helper nntp sync --dry-run
./helper nntp sync
./helper nntp sync --group comp.sys.amiga.misc
```

`--group` selects an enabled group; it does not override disabled groups.
Dry runs fetch and decode articles, but do not write messages, checkpoints,
or run-history rows. Opening a fresh state store may initialize its schema.

Add an event with command `{BBS_ROOT}/helper`, arguments `["nntp", "sync"]`,
working directory `{BBS_ROOT}`, timeout 240 seconds, and an hourly schedule.
Keep the event timeout above `max_run_seconds`. One connection serves all
groups. A process lock prevents overlapping manual and scheduled imports.

## History and threading

The first run reads the latest `initial_articles` article-number slots per
group. Expired holes and skipped articles can reduce the number imported.
Later runs continue from a high-water checkpoint, scanning at most
`max_articles_per_sync` slots per group. A server that resets its numbering
to below the checkpoint starts again from the recent window.

Original Message-IDs prevent duplicate messages even if a checkpoint is lost.
Original dates, subjects, author names, and the latest 64 References are retained. A missing
time zone from an older reader is treated as UTC; an unparseable/missing date
uses import time. Replies link through JAM ReplyTo and ReplyID. Parents outside
the imported window are not automatically fetched; their references remain
available, and the reader can resolve them if the parent later arrives.

Checkpoints and run history live alongside other gateways in
`data/source/source.db`, under the `nntp` source namespace. Existing Reddit and
web-source state is unchanged. Crossposted articles appear once in each
configured area, rather than being suppressed across different newsgroups.

## Text and limits

The importer decodes MIME headers, quoted-printable/base64 text, multipart
messages (preferring inline plain text), common legacy encodings, and CP437.
HTML-only text is converted to plain text. Terminal escape/control sequences
are removed. MIME attachments, control articles, and unsupported non-text
articles are skipped. The known legacy `charset=us - ascii` spelling is accepted.

Articles advertised above `max_article_bytes` are skipped. Network responses,
lines, decoded text, MIME nesting, and run time are bounded. A network failure
or unexpected response leaves the checkpoint unchanged; already-written
messages are still protected by Message-ID deduplication on retry. A server
that sends more data than advertised may cause the run to fail at the size
limit instead of skipping that article.
