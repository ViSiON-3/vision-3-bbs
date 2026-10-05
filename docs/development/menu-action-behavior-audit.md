# Menu Action Behavior Audit Ledger

Source mappings: [`menu-action-coverage.json`](menu-action-coverage.json). One row per distinct shipped `RUN` target; inspect every linked test named by the source mapping before marking a row verified.

**Progress:** 91 targets; 41 verified; 0 open gaps recorded; 50 unreviewed. The linked test counts below are distinct test functions per target and may overlap across targets.

| RUN target | Shipped action rows | Linked test functions | Audited outcomes / gaps | Status |
|---|---|---:|---|---|
| `ADMINLISTUSERS` | ADMIN[1] E | 11 | Edits persist and produce old/new audit entries; password is masked and hashed; invalid levels and protected user 1 are rejected; cancel/discard/blank-handle/disconnect do not save unintended changes. | verified |
| `AUTHENTICATE` | LOGIN[1]  | 8 | Successful login, password masking, backspace, login timestamp and lockout-counter clearing; wrong password/counting, locked IP, logon-level denial, abort/disconnect, existing session, and signup continuation are asserted. | verified |
| `BATCHDOWNLOAD` | FILEM[1] B | 5 | — | unreviewed |
| `BBSLIST` | BBSLISTM[0] L | 6 | Caller/sysop browser permissions, details, navigation, live edit/delete/verify, empty/deleted-last-item, disconnect, and Unicode column clipping are checked. | verified |
| `BBSLISTADD` | BBSLISTM[1] A | 4 | Complete entries are persisted with trimming/defaults/IDs and rune-safe field caps; blank required fields abort without creating the data file. | verified |
| `BBSLISTDELETE` | BBSLISTM[3] D | 1 | Empty/invalid/refused/declined paths preserve entries; confirmed CoSysOp deletion compacts IDs/list and keeps NextID behavior. | verified |
| `BBSLISTEDIT` | BBSLISTM[2] C | 4 | Owner/sysop permissions, validation, field edits, caps, and disconnect are asserted against persisted records. | verified |
| `BBSLISTVERIFY` | BBSLISTM[4] V | 1 | CoSysOp gate, empty/invalid choices, and both persisted verified/unverified transitions are asserted. | verified |
| `CFG_AUTOSIG` | MAIN[16] U<br>MSGMENU[16] S | 2 | Create/delete persists; line cap and abandon behavior preserve expected value; empty-delete and disconnect notices are checked. | verified |
| `CFG_FILECOLUMNS` | FILEM[12] K | 1 | Toggles save the expected column set; the final required column stays enabled; disconnect does not save. | verified |
| `CFG_PASSWORD` | MAIN[25] + | 2 | Wrong current password and disconnect preserve the old hash; valid change is masked, persisted as a new hash, and verifies only for the new password. | verified |
| `CHANGEFILECONF` | FILEM[2] C | 6 | Login/config/template guards; accessible conference selection updates file and message conference/area state on disk; quit and save failure preserve prior selection. | verified |
| `CHANGEMSGCONF` | MSGMENU[1] C | 4 | — | unreviewed |
| `CHAT` | MAIN[3] C<br>MAIN[24] ! | 1 | — | unreviewed |
| `CLEAR_BATCH` | FILEM[24] - | 2 | — | unreviewed |
| `COMPOSEMSG` | MSGMENU[10] P | 9 | Public/private posts assert stored author/recipient/body/privacy and counters; signature/anonymity/real-name rules and abort/access refusals are covered. | verified |
| `DOWNLOADFILE` | FILEM[7] D | 3 | — | unreviewed |
| `EDITFILERECORD` | FILEM[8] E | 10 | Description, rename, move, review, and delete effects are reloaded from disk; invalid/declined/failed operations preserve records/files; CoSysOp gate and audit-screen details checked. | verified |
| `EDITNEWS` | ADMIN[5] W | 10 | — | unreviewed |
| `FILENEWSCANCONFIG` | FILEM[22] Z | 3 | Toggle/navigation/all/none choices persist; inaccessible-area and disconnect paths do not save unintended tags. | verified |
| `FILE_NEWSCAN` | FILEM[14] N | 3 | Cutoff boundary, area grouping, access/tags/current-area filters, empty results, and long-list paging are asserted. | verified |
| `FULL_LOGIN_SEQUENCE` | FASTLOGN[0] 1 | 1 | — | unreviewed |
| `GETHEADERTYPE` | MSGMENU[13] H | 3 | — | unreviewed |
| `IMMEDIATELOGOFF` | BBSLISTM[7] /G<br>DOORSM[9] /G<br>FASTLOGN[5] /G<br>INFORMM[6] /G<br>MAIN[29] /G<br>MSGMENU[19] /G<br>RUMORM[8] /G<br>V3NETM[8] /G | 1 | — | unreviewed |
| `INFOFORMHUNT` | INFORMM[2] H | 3 | Sysop gate protects response data; all-user results are matched to the requested form; empty, invalid, missing-template and no-response paths are covered. | verified |
| `INFOFORMNUKE` | INFORMM[3] * | 2 | Confirmed action removes all forms for the selected user but preserves other users; non-sysop, unknown/blank user and declined confirmation preserve data. | verified |
| `INFOFORMS` | INFORMM[0] I | 9 | Answers persist only on completion; required/refill/min-level rules are asserted; view output and pagination checked; disconnect and refusals avoid partial or unauthorized data. | verified |
| `INFOFORMVIEW` | INFORMM[1] V | 3 | Invalid numbers, missing response/template, replayed answers, pagination and stop/continue behavior are asserted. | verified |
| `LASTCALLERS` | MAIN[20] W LC | 2 | — | unreviewed |
| `LISTFILES` | FILEM[9] F L | 19 | — | unreviewed |
| `LISTFILES_EXTENDED` | FILEM[20] W | 1 | — | unreviewed |
| `LISTMSGAR` | MSGMENU[0] * | 4 | — | unreviewed |
| `LISTMSGS` | MSGMENU[12] L | 7 | Private mail from other users is omitted from lists even when the reader passes area ACS or adopts a recipient's real name; public posts and the user's own private mail remain visible. Empty/no-area/anonymous guards, selection and paging, read-pointer persistence, redraw, deletion, and UTF-8 fields are asserted. | verified |
| `LISTNEWS` | MAIN[2] J<br>MAIN[10] N | 4 | Visible list, level filtering, invalid selection, NEW marker, seen-item persistence, empty/hidden-news and corrupt-data paths are asserted. | verified |
| `LISTNUV` | ADMIN[6] U | 5 | — | unreviewed |
| `LISTPRIVMAIL` | EMAILM[2] L | 2 | Only the caller's private messages are listed; opening a selected message restores the prior area, and logged-out filtering rejects private mail. | verified |
| `LISTUSERS` | MAIN[8] L | 2 | — | unreviewed |
| `MAILPOLL` | ADMIN[7] M | 3 | — | unreviewed |
| `MAINLOGOFF` | BBSLISTM[6] G<br>DOORSM[8] G<br>FASTLOGN[4] G<br>INFORMM[5] G<br>MAIN[5] G<br>MSGMENU[18] G<br>RUMORM[7] G<br>V3NETM[7] G | 1 | — | unreviewed |
| `NEWSCAN` | MSGMENU[7] N | 9 | Tagged/conference scope; skip, quit, nonstop, jump, posting, and no-tags paths; shown messages and persisted last-read/current-area pointers, including update-pointers-off. Privacy tests confirm scans show public notes but hide other users' private mail. | verified |
| `NEWSCANCONFIG` | MSGMENU[8] Z<br>QWKM[0] C | 2 | Readable-area filtering; keyboard navigation, toggle/all/none actions, save notice, and persisted tag list. | verified |
| `NEXTFILEAREA` | FILEM[3] ] | 2 | Wraparound and visible notice; state persisted each step; caller cannot navigate into an inaccessible area. Linked the caller permission test during audit. | verified |
| `NEXTFILECONF` | FILEM[5] } | 1 | Conference transition clears nonexistent areas and synchronizes message/file conference state; visible destination notice. | verified |
| `NEXTMSGAREA` | MSGMENU[3] ] | 1 | — | unreviewed |
| `NEXTMSGCONF` | MSGMENU[5] } | 2 | — | unreviewed |
| `ONELINER` | MAIN[11] O | 7 | — | unreviewed |
| `PAGE` | MAIN[28] P | 1 | — | unreviewed |
| `PENDINGVALIDATIONNOTICE` | MAIN[0] // | 1 | — | unreviewed |
| `PREVFILEAREA` | FILEM[4] [ | 2 | Wraparound and visible notice; state persisted each step; caller cannot navigate into an inaccessible area. Linked the caller permission test during audit. | verified |
| `PREVFILECONF` | FILEM[6] { | 1 | Return transition restores Local conference and both current area selections on disk. | verified |
| `PREVMSGAREA` | MSGMENU[4] [ | 1 | — | unreviewed |
| `PREVMSGCONF` | MSGMENU[6] { | 1 | — | unreviewed |
| `PURGEUSERS` | ADMIN[3] P | 4 | Retention cutoff, disabled/empty cases, and declined confirmation preserve users; confirmed purge deletes only eligible accounts and records audit entries. | verified |
| `QWKDOWNLOAD` | QWKM[1] D | 7 | Success validates delivered archive members and last-read pointer; failure/cancel keeps pointer unchanged and cleans staging files; empty/tagged/build/space/rename cases asserted. | verified |
| `QWKUPLOAD` | QWKM[2] U | 6 | Valid replies post with signature and counters; duplicate upload is idempotent; write ACS and malformed/foreign/empty/transfer/temp/unreadable failures assert no unintended posts and cleanup. | verified |
| `RANDOMRUMOR` | RUMORM[5] * | 1 | — | unreviewed |
| `READMSGS` | MSGMENU[11] R | 25 | Reader navigation, jump/thread, header rendering, scroll/encoding, post/reply/delete/help/list actions, and last-read persistence are asserted. Privacy cases keep others' mail and real-name-only mail hidden while retaining public posts and each party's own mail. | verified |
| `READPRIVMAIL` | EMAILM[1] R | 9 | Mailbox filtering, first unread, empty/logged-out states, private replies and round trips, legacy real-name sender resolution, ambiguous/anonymous/deleted sender refusal, and reply-then-next navigation are asserted. | verified |
| `RUMORSADD` | RUMORM[1] A | 4 | — | unreviewed |
| `RUMORSDELETE` | RUMORM[4] D | 6 | — | unreviewed |
| `RUMORSLIST` | RUMORM[0] L | 5 | — | unreviewed |
| `RUMORSNEWSCAN` | RUMORM[2] N | 1 | — | unreviewed |
| `RUMORSSEARCH` | RUMORM[3] S | 2 | — | unreviewed |
| `SCANNUV` | MAIN[18] H NUV | 6 | — | unreviewed |
| `SEARCH_FILES` | FILEM[16] S | 3 | — | unreviewed |
| `SELECTFILEAREA` | FILEM[0] A * | 4 | — | unreviewed |
| `SELECTMSGAREA` | MSGMENU[2] A | 3 | — | unreviewed |
| `SENDPRIVMAIL` | EMAILM[0] S | 3 | Successful delivery persists private status, handle identities, subject/body/signature, and sender count; blank/unknown/deleted recipients, aborts, and logged-out use do not write mail. | verified |
| `SETFILESCANDATE` | FILEM[23] Y | 2 | Valid date/all/reset choices persist; invalid, blank, and ESC cancellation preserve the old cutoff; disconnect/no-user paths checked. | verified |
| `SHOWFILEINFO` | FILEM[11] I | 2 | — | unreviewed |
| `SHOWSTATS` | MAIN[21] Y | 3 | — | unreviewed |
| `SHOWVERSION` | MAIN[26] ^ | 1 | — | unreviewed |
| `SPONSOREDITAREA` | SPONSORM[0] E | 20 | All fields, validation, permissions, tri-state/boolean values, rename and navigation are checked; edits persist, while discard/rejection/save failure roll back live and disk state. | verified |
| `SPONSORMENU` | MSGMENU[14] % | 13 | Access gates, navigation/wrap, sponsor filtering, repositioning, invalid input, cancel/disconnect, save failure, and editor return are asserted; order/current area is reloaded from disk. | verified |
| `SYSTEMSTATS` | MAIN[14] S | 2 | — | unreviewed |
| `TOGGLEALLOWNEWUSERS` | ADMIN[2] N | 1 | — | unreviewed |
| `TYPE_TEXT_FILE` | FILEM[17] T | 1 | Text and archive bytes are displayed as raw text with end marker; blank input opens nothing. | verified |
| `UPDATENEWSCAN` | MSGMENU[9] U | 4 | Mark-all-read/all-new/date and current-conference scope are verified in persisted pointers; blank/invalid/ESC and anonymous paths preserve or refuse changes. | verified |
| `UPLOADFILE` | FILEM[18] U | 2 | — | unreviewed |
| `USERCONFIG` | MAIN[7] K | 4 | Autosignature create/delete/truncate/cancel, header selection, and screen-height validation assert rendered feedback and reloaded saved state. | verified |
| `V3NETACCESSREQUESTS` | V3NETM[3] R | 1 | — | unreviewed |
| `V3NETAREAS` | V3NETM[1] A | 4 | — | unreviewed |
| `V3NETCOORDINATOR` | V3NETM[4] C | 1 | — | unreviewed |
| `V3NETPROPOSE` | V3NETM[2] P | 4 | — | unreviewed |
| `V3NETREGISTRY` | V3NETM[5] N | 1 | — | unreviewed |
| `V3NETSTATUS` | V3NETM[0] S | 1 | — | unreviewed |
| `VALIDATEUSER` | ADMIN[0] V | 3 | Only pending users are listed; selection validates the intended account, updates its level and audit entry; empty queue and remaining-user behavior checked. | verified |
| `VIEW_FILE` | FILEM[19] V | 3 | — | unreviewed |
| `VOTE` | ADMIN[4] T<br>MAIN[17] V VOTE | 6 | — | unreviewed |
| `WANTLIST` | FILEM[21] X | 4 | — | unreviewed |
| `WHOISONLINE` | MAIN[27] /W | 3 | — | unreviewed |

## Review criteria

- **verified** only after inspecting the linked tests and recording observable success/failure outcomes they assert.
- **gap** when an expected outcome or state transition has no meaningful assertion; record the missing case.
- **deferred** when a terminal journey is intentionally postponed, with the fixture/safety reason.
- **unreviewed** until that review is complete. Test names or AST references alone do not count as verification.

Keep terminal UI disposition separate from unit behavior status. The inventory provides terminal journey links; this ledger tracks behavior assertion review.
