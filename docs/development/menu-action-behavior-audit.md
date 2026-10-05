# Menu Action Behavior Audit Ledger

Source mappings: [`menu-action-coverage.json`](menu-action-coverage.json). One row per distinct shipped `RUN` target; inspect every linked test named by the source mapping before marking a row verified.

**Progress:** 91 targets; 91 verified; 0 open gaps; 0 unreviewed. The linked test counts below are distinct test functions per target and may overlap across targets.

| RUN target | Shipped action rows | Linked test functions | Audited outcomes / gaps | Status |
|---|---|---:|---|---|
| `ADMINLISTUSERS` | ADMIN[1] E | 11 | Edits persist and produce old/new audit entries; password is masked and hashed; invalid levels and protected user 1 are rejected; cancel/discard/blank-handle/disconnect do not save unintended changes. | verified |
| `AUTHENTICATE` | LOGIN[1]  | 8 | Successful login, password masking, backspace, login timestamp and lockout-counter clearing; wrong password/counting, locked IP, logon-level denial, abort/disconnect, existing session, and signup continuation are asserted. | verified |
| `BATCHDOWNLOAD` | FILEM[1] B | 5 | Successful transfers clear and persist the batch and increment user/file counters; failed transfers do not count; protocol cancellation/no-protocol paths preserve the batch; stale IDs are cleared without offering a transfer. | verified |
| `BBSLIST` | BBSLISTM[0] L | 6 | Caller/sysop browser permissions, details, navigation, live edit/delete/verify, empty/deleted-last-item, disconnect, and Unicode column clipping are checked. | verified |
| `BBSLISTADD` | BBSLISTM[1] A | 4 | Complete entries are persisted with trimming/defaults/IDs and rune-safe field caps; blank required fields abort without creating the data file. | verified |
| `BBSLISTDELETE` | BBSLISTM[3] D | 1 | Empty/invalid/refused/declined paths preserve entries; confirmed CoSysOp deletion compacts IDs/list and keeps NextID behavior. | verified |
| `BBSLISTEDIT` | BBSLISTM[2] C | 4 | Owner/sysop permissions, validation, field edits, caps, and disconnect are asserted against persisted records. | verified |
| `BBSLISTVERIFY` | BBSLISTM[4] V | 1 | CoSysOp gate, empty/invalid choices, and both persisted verified/unverified transitions are asserted. | verified |
| `CFG_AUTOSIG` | MAIN[16] U<br>MSGMENU[16] S | 2 | Create/delete persists; line cap and abandon behavior preserve expected value; empty-delete and disconnect notices are checked. | verified |
| `CFG_FILECOLUMNS` | FILEM[12] K | 1 | Toggles save the expected column set; the final required column stays enabled; disconnect does not save. | verified |
| `CFG_PASSWORD` | MAIN[25] + | 2 | Wrong current password and disconnect preserve the old hash; valid change is masked, persisted as a new hash, and verifies only for the new password. | verified |
| `CHANGEFILECONF` | FILEM[2] C | 6 | Login/config/template guards; accessible conference selection updates file and message conference/area state on disk; quit and save failure preserve prior selection. | verified |
| `CHANGEMSGCONF` | MSGMENU[1] C | 5 | Lightbar and classic selectors render, accept/reject inputs, and quit without mutation; joining persists both message/file conference state and clears invalid area state; save failure rolls back memory and disk state. | verified |
| `CHAT` | MAIN[3] C<br>MAIN[24] ! | 2 | Teleconference snoop mode enters and restores; the local chat journey asserts message echo/history, room and topic commands, scrollback, and room cleanup on exit. | verified |
| `CLEAR_BATCH` | FILEM[24] - | 2 | Nonempty queues clear and persist with a count; empty queues report empty and remain unchanged. | verified |
| `COMPOSEMSG` | MSGMENU[10] P | 9 | Public/private posts assert stored author/recipient/body/privacy and counters; signature/anonymity/real-name rules and abort/access refusals are covered. | verified |
| `DOWNLOADFILE` | FILEM[7] D | 4 | Valid case-insensitive filenames are added and persisted; blank input cancels, batch-menu add-more works, and an unknown name shows not-found feedback before a valid retry leaves only the valid ID persisted. | verified |
| `EDITFILERECORD` | FILEM[8] E | 10 | Description, rename, move, review, and delete effects are reloaded from disk; invalid/declined/failed operations preserve records/files; CoSysOp gate and audit-screen details checked. | verified |
| `EDITNEWS` | ADMIN[5] W | 10 | CoSysOp gate; add/edit/delete persist fields and IDs; cancelled/disconnected adds and edits do not save; invalid levels/selections are rejected; Unicode title caps, list/view, and empty-state behavior are asserted. | verified |
| `FILENEWSCANCONFIG` | FILEM[22] Z | 3 | Toggle/navigation/all/none choices persist; inaccessible-area and disconnect paths do not save unintended tags. | verified |
| `FILE_NEWSCAN` | FILEM[14] N | 3 | Cutoff boundary, area grouping, access/tags/current-area filters, empty results, and long-list paging are asserted. | verified |
| `FULL_LOGIN_SEQUENCE` | FASTLOGN[0] 1 | 6 | Login items run in order with clear/pause/level rules; unknown/missing items are skipped; door and script outcomes control continuation/GOTO/logoff; shipped sequences are dispatchable and put priority items before FASTLOGIN. | verified |
| `GETHEADERTYPE` | MSGMENU[13] H | 3 | Selection, preview acceptance/decline, quit preservation, persistence, UTF-8/CP437 rendering, and logged-out no-op are checked. | verified |
| `IMMEDIATELOGOFF` | BBSLISTM[7] /G<br>DOORSM[9] /G<br>FASTLOGN[5] /G<br>INFORMM[6] /G<br>MAIN[29] /G<br>MSGMENU[19] /G<br>RUMORM[8] /G<br>V3NETM[8] /G | 1 | Immediate action skips confirmation and displays GOODBYE.ANS; absent screen falls back to configured goodbye text without leaking the file-load error. | verified |
| `INFOFORMHUNT` | INFORMM[2] H | 3 | Sysop gate protects response data; all-user results are matched to the requested form; empty, invalid, missing-template and no-response paths are covered. | verified |
| `INFOFORMNUKE` | INFORMM[3] * | 2 | Confirmed action removes all forms for the selected user but preserves other users; non-sysop, unknown/blank user and declined confirmation preserve data. | verified |
| `INFOFORMS` | INFORMM[0] I | 9 | Answers persist only on completion; required/refill/min-level rules are asserted; view output and pagination checked; disconnect and refusals avoid partial or unauthorized data. | verified |
| `INFOFORMVIEW` | INFORMM[1] V | 3 | Invalid numbers, missing response/template, replayed answers, pagination and stop/continue behavior are asserted. | verified |
| `LASTCALLERS` | MAIN[20] W LC | 2 | Visible recent callers render oldest-to-newest with duration/note and user count; invisible sessions are hidden; numeric argument limits rows, junk uses the default, and disconnect logs off. | verified |
| `LISTFILES` | FILEM[9] F L | 21 | Classic/lightbar paging, selection, text/archive viewing, persisted tagging, caller/sysop permissions, file edit/delete/move/rename, cancel/failure paths, and invalid area guards are asserted. Both listing modes initiate a successful local transfer and persist cleared tags plus user/file download counters. | verified |
| `LISTFILES_EXTENDED` | FILEM[20] W | 1 | Extended listing displays description/size columns regardless of saved column choices. | verified |
| `LISTMSGAR` | MSGMENU[0] * | 4 | Lists only the current conference and readable areas; empty-list colors render correctly; pause waits for Enter and reports disconnect. | verified |
| `LISTMSGS` | MSGMENU[12] L | 7 | Private mail from other users is omitted from lists even when the reader passes area ACS or adopts a recipient's real name; public posts and the user's own private mail remain visible. Empty/no-area/anonymous guards, selection and paging, read-pointer persistence, redraw, deletion, and UTF-8 fields are asserted. | verified |
| `LISTNEWS` | MAIN[2] J<br>MAIN[10] N | 4 | Visible list, level filtering, invalid selection, NEW marker, seen-item persistence, empty/hidden-news and corrupt-data paths are asserted. | verified |
| `LISTNUV` | ADMIN[6] U | 5 | Candidate tallies and viewer vote state render; sysops can add/remove candidates while invalid/unknown entries are rejected; vote-screen navigation, changing votes, comments, and infoform paths are checked against persisted queue state. | verified |
| `LISTPRIVMAIL` | EMAILM[2] L | 2 | Only the caller's private messages are listed; opening a selected message restores the prior area, and logged-out filtering rejects private mail. | verified |
| `LISTUSERS` | MAIN[8] L | 2 | Live users and validation counts render; deleted/banned accounts are excluded; disconnect at the closing pause logs off. | verified |
| `MAILPOLL` | ADMIN[7] M | 10 | Child args/cwd/output/exit status and missing executable render correctly; success/failure, stop keys/disconnect with SIGTERM, timeout sizing, and split UTF-8/CRLF output are checked. | verified |
| `MAINLOGOFF` | BBSLISTM[6] G<br>DOORSM[8] G<br>FASTLOGN[4] G<br>INFORMM[5] G<br>MAIN[5] G<br>MSGMENU[18] G<br>RUMORM[7] G<br>V3NETM[7] G | 1 | Yes confirms and displays GOODBYE.ANS; No and Enter-default-No stay in the session; disconnect logs off; fallback goodbye text is displayed when the screen is absent. | verified |
| `NEWSCAN` | MSGMENU[7] N | 9 | Tagged/conference scope; skip, quit, nonstop, jump, posting, and no-tags paths; shown messages and persisted last-read/current-area pointers, including update-pointers-off. Privacy tests confirm scans show public notes but hide other users' private mail. | verified |
| `NEWSCANCONFIG` | MSGMENU[8] Z<br>QWKM[0] C | 2 | Readable-area filtering; keyboard navigation, toggle/all/none actions, save notice, and persisted tag list. | verified |
| `NEXTFILEAREA` | FILEM[3] ] | 2 | Wraparound and visible notice; state persisted each step; caller cannot navigate into an inaccessible area. Linked the caller permission test during audit. | verified |
| `NEXTFILECONF` | FILEM[5] } | 1 | Conference transition clears nonexistent areas and synchronizes message/file conference state; visible destination notice. | verified |
| `NEXTMSGAREA` | MSGMENU[3] ] | 2 | Navigation wraps within the conference, reports/persists the current area, and skips unreadable private areas for callers. | verified |
| `NEXTMSGCONF` | MSGMENU[5] } | 2 | Conference navigation wraps and persists state; joining clears nonexistent message/file areas and synchronizes both conference selections. | verified |
| `ONELINER` | MAIN[11] O | 9 | Newest-ten display and anonymity, caller/sysop posting, safe colors, blank/declined/disconnected writes, DataDir persistence, and rune-safe truncation are asserted. | verified |
| `PAGE` | MAIN[28] P | 5 | A valid target receives the trimmed page and confirmation; invalid/self/offline/invisible targets, blank message, prompt cancellation, and disconnect do not queue a page. | verified |
| `PENDINGVALIDATIONNOTICE` | MAIN[0] // | 1 | Sysops see the pending count only when users are pending; callers, logged-out sessions, banned accounts, and empty queues produce no notice. | verified |
| `PREVFILEAREA` | FILEM[4] [ | 2 | Wraparound and visible notice; state persisted each step; caller cannot navigate into an inaccessible area. Linked the caller permission test during audit. | verified |
| `PREVFILECONF` | FILEM[6] { | 1 | Return transition restores Local conference and both current area selections on disk. | verified |
| `PREVMSGAREA` | MSGMENU[4] [ | 2 | Navigation wraps within the conference, reports/persists the current area, and skips unreadable private areas for callers. | verified |
| `PREVMSGCONF` | MSGMENU[6] { | 2 | Return navigation wraps and restores the Local conference plus default message/file area selections on disk. | verified |
| `PURGEUSERS` | ADMIN[3] P | 4 | Retention cutoff, disabled/empty cases, and declined confirmation preserve users; confirmed purge deletes only eligible accounts and records audit entries. | verified |
| `QWKDOWNLOAD` | QWKM[1] D | 7 | Success validates delivered archive members and last-read pointer; failure/cancel keeps pointer unchanged and cleans staging files; empty/tagged/build/space/rename cases asserted. | verified |
| `QWKUPLOAD` | QWKM[2] U | 6 | Valid replies post with signature and counters; duplicate upload is idempotent; write ACS and malformed/foreign/empty/transfer/temp/unreadable failures assert no unintended posts and cleanup. | verified |
| `RANDOMRUMOR` | RUMORM[5] * | 1 | Selects only a visible rumor, centers the output, stays quiet on an empty board, and does nothing when logged out. | verified |
| `READMSGS` | MSGMENU[11] R | 25 | Reader navigation, jump/thread, header rendering, scroll/encoding, post/reply/delete/help/list actions, and last-read persistence are asserted. Privacy cases keep others' mail and real-name-only mail hidden while retaining public posts and each party's own mail. | verified |
| `READPRIVMAIL` | EMAILM[1] R | 9 | Mailbox filtering, first unread, empty/logged-out states, private replies and round trips, legacy real-name sender resolution, ambiguous/anonymous/deleted sender refusal, and reply-then-next navigation are asserted. | verified |
| `RUMORSADD` | RUMORM[1] A | 5 | Anonymous identity/ownership, level validation, pipe defusing, generated IDs/timestamps, blank/low-level/full refusals, and corrupt-file reporting are checked. | verified |
| `RUMORSDELETE` | RUMORM[4] D | 7 | Owner/sysop permissions, confirmation/decline, legacy owner migration, hidden rumor privacy, invalid/empty/disconnect cases, and corrupt-file reporting preserve expected IDs and state. | verified |
| `RUMORSLIST` | RUMORM[0] L | 6 | Level filtering, anonymous masking/unmasking, wide-column clipping, empty/logged-out cases, DataDir lookup, and corrupt-file reporting are asserted. | verified |
| `RUMORSNEWSCAN` | RUMORM[2] N | 2 | Filters by previous login, handles first-time/no-new callers, hides high-level rumors, and reports corrupt data. | verified |
| `RUMORSSEARCH` | RUMORM[3] S | 3 | Case-insensitive text/author search, level filtering, anonymous masking/unmasking, empty/no-match cases, and corrupt-file reporting are asserted. | verified |
| `SCANNUV` | MAIN[18] H NUV | 6 | Feature and access gates, empty/corrupt/already-voted queues, vote comments and persistence, yes/no threshold outcomes, validation/deletion, and missing-account preservation are asserted. | verified |
| `SEARCH_FILES` | FILEM[16] S | 4 | Search matches names/descriptions across readable areas only; short/blank/no-match and logged-out inputs are handled; long result lists page; UTF-8 filenames are clamped on rune boundaries. | verified |
| `SELECTFILEAREA` | FILEM[0] A * | 4 | Lightbar navigation spans pages and conferences without joining; Enter joins and persists the area; classic prompt supports relist/quit, filters inaccessible areas, and persists valid selection. | verified |
| `SELECTMSGAREA` | MSGMENU[2] A | 6 | Lightbar and classic modes support navigation/conference filtering, access denial, quit/disconnect, and persisted area selection. | verified |
| `SENDPRIVMAIL` | EMAILM[0] S | 3 | Successful delivery persists private status, handle identities, subject/body/signature, and sender count; blank/unknown/deleted recipients, aborts, and logged-out use do not write mail. | verified |
| `SETFILESCANDATE` | FILEM[23] Y | 2 | Valid date/all/reset choices persist; invalid, blank, and ESC cancellation preserve the old cutoff; disconnect/no-user paths checked. | verified |
| `SHOWFILEINFO` | FILEM[11] I | 2 | Case-insensitive lookup displays persisted record size/date/uploader/download count/area/description; no-user/no-area/blank/unknown/disconnect paths are checked. | verified |
| `SHOWSTATS` | MAIN[21] Y | 3 | User placeholders, time remaining/unlimited, UTF-8/CP437 screen bytes, fallback pause, missing-screen error, logged-out refusal, and pause disconnect are checked. | verified |
| `SHOWVERSION` | MAIN[26] ^ | 1 | Clears screen and renders version/pipe codes; pauses when configured, uses goodbye behavior on disconnect, and returns without waiting when pause is empty. | verified |
| `SPONSOREDITAREA` | SPONSORM[0] E | 20 | All fields, validation, permissions, tri-state/boolean values, rename and navigation are checked; edits persist, while discard/rejection/save failure roll back live and disk state. | verified |
| `SPONSORMENU` | MSGMENU[14] % | 13 | Access gates, navigation/wrap, sponsor filtering, repositioning, invalid input, cancel/disconnect, save failure, and editor return are asserted; order/current area is reloaded from disk. | verified |
| `SYSTEMSTATS` | MAIN[14] S | 2 | Board name, sysop, user/call totals, and active-node counts are matched to seeded state; logged-out sessions see no screen. | verified |
| `TOGGLEALLOWNEWUSERS` | ADMIN[2] N | 1 | Sysop toggle updates live config, persists `AllowNewUsers` to disk, and reports the new state; logged-out invocation has no effect. | verified |
| `TYPE_TEXT_FILE` | FILEM[17] T | 3 | Text and archive bytes are displayed raw with end marker; blank input opens nothing; shared paging preserves all lines and rejects oversized files. | verified |
| `UPDATENEWSCAN` | MSGMENU[9] U | 4 | Mark-all-read/all-new/date and current-conference scope are verified in persisted pointers; blank/invalid/ESC and anonymous paths preserve or refuse changes. | verified |
| `UPLOADFILE` | FILEM[18] U | 3 | Login/area/ACS gates, cancellation/disconnect cleanup, and a successful local receive are asserted; the received bytes, file record metadata, uploader credit, and staging cleanup are verified. | verified |
| `USERCONFIG` | MAIN[7] K | 4 | Autosignature create/delete/truncate/cancel, header selection, and screen-height validation assert rendered feedback and reloaded saved state. | verified |
| `V3NETACCESSREQUESTS` | V3NETM[3] R | 9 | Managed-area filtering, relative ages, approve/deny payloads, reason forwarding, invalid commands, list failures, approve/deny failures with retained requests, no-managed-area, parser, and nil-NAL cases are covered. | verified |
| `V3NETAREAS` | V3NETM[1] A | 5 | Subscribe/unsubscribe persists leaves and local areas; auto-join choice, reload/no-reload failures, NAL errors/empty state, paging/disconnect, and rune-safe row rendering are checked. | verified |
| `V3NETCOORDINATOR` | V3NETM[4] C | 7 | Non-coordinator gate, proposal listing/approve/reject/mode override, manager reassignment and errors, hub listing errors, and approve/reject failures with retained proposals are checked. | verified |
| `V3NETPROPOSE` | V3NETM[2] P | 5 | Editable form submission, access-mode cycling, required/malformed field validation, hub errors, wrap navigation, cancel/disconnect, no-network and disabled states are asserted. | verified |
| `V3NETREGISTRY` | V3NETM[5] N | 2 | Registry entries, descriptions, URLs, counts, and subscription markers render; HTTP failure shows an error screen. | verified |
| `V3NETSTATUS` | V3NETM[0] S | 2 | Configured node/hub/subscription state renders; disabled service shows an explicit disabled-state screen. | verified |
| `VALIDATEUSER` | ADMIN[0] V | 3 | Only pending users are listed; selection validates the intended account, updates its level and audit entry; empty queue and remaining-user behavior checked. | verified |
| `VIEW_FILE` | FILEM[19] V | 7 | Text viewing, archive listing, multi-page/early-quit behavior, blank/unknown/no-area/no-user/disconnect and missing-area paths are asserted; shared paging checks line integrity and oversized-file rejection. | verified |
| `VOTE` | ADMIN[4] T<br>MAIN[17] V VOTE | 6 | Topic creation and deletion, choice additions, navigation/listing, results gating, invalid choices, and one-vote-per-user are checked against persisted voting data. | verified |
| `WANTLIST` | FILEM[21] X | 6 | Caller requests persist with IDs and reasons; sysop listing/deletion/clear preserves NextID; corrupt data is preserved and surfaced; DataDir routing and concurrent stale/duplicate deletion are covered. | verified |
| `WHOISONLINE` | MAIN[27] /W | 3 | Regular callers cannot see invisible sessions; CoSysOps can; node counts and fixed-width handle fields are checked; disconnect logs off. | verified |

## Review criteria

- **verified** only after inspecting the linked tests and recording observable success/failure outcomes they assert.
- **gap** when an expected outcome or state transition has no meaningful assertion; record the missing case.
- **deferred** when a terminal journey is intentionally postponed, with the fixture/safety reason.
- **unreviewed** until that review is complete. Test names or AST references alone do not count as verification.

Keep terminal UI disposition separate from unit behavior status. The inventory provides terminal journey links; this ledger tracks behavior assertion review.
