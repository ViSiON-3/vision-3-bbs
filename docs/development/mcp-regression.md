# MCP terminal regression suite

The terminal regression MCP is a small fork of the terminal and ANSI screen
model used by the `v3agents` project. It drives a real ViSiON/3 server over
Telnet or SSH and returns the rendered terminal screen after ANSI cursor
movement, erase, and redraw operations.

The live suite is the black-box UI layer: it logs in through MCP, sends the
same keys a caller would type, and checks stable screen labels and transitions.
The ordinary package tests cover the MCP session, key parser, and VT screen
model without needing a running BBS. Existing package tests cover command-level
details and data mutations.

## Coverage standard

Every available menu binding should have a test proving that its key dispatches
to the intended command and a behavioral test proving the command's outcome.
Those can be the same test when practical. A binding-level test should load or
otherwise exercise the shipped menu configuration; a test of a handler in
isolation does not prove that the menu is wired to it. The test should check
what the action is meant to do, not just that its menu opens. Choose the test
layer based on the behavior:

- Unit tests cover calculations, validation, parsing, and state transitions.
- Integration tests cover interactions with storage, files, protocols, and
  other services using isolated fixtures or fakes.
- Live terminal journeys cover caller-visible prompts, keys, screen changes,
  and representative end-to-end flows on a disposable board. Use these for
  navigation and visible UI behavior, not as the only way to test every
  mutation or external-service action.

An exhaustive suite does not need to run every action through one live BBS
session. Some actions intentionally mutate data, require another caller, move
files, or launch external services. Those actions still need tests, with
controlled state and observable outcomes. Cover alternate outcomes and edge
cases at the unit or integration layer; cover each shipped binding's dispatch
at a fast config-to-executor test layer; and reserve live journeys for
representative end-to-end UI paths. This keeps action coverage complete without
making the live suite destructive, slow, or dependent on unavailable services.

The checked-in
[`menu-action-coverage.json`](menu-action-coverage.json) inventory lists every
shipped menu binding and links current terminal journeys and command-handler
tests in `internal/menu`. The inventory test fails if the list drifts from
`menus/v3/cfg` or a referenced test is renamed, and it checks that `RUN:` rows
link to a test of the registered handler. It currently records all 151
bindings: 92 with terminal journeys, 137 with handler-test references, and
none without a test reference. A separate shipped-config matcher test now
checks all 153 selectable key bindings across those menu configs and confirms
each resolves to its configured command. A second test follows the 148 direct
`RUN:`, `DOOR:`, `GOTO:`, and `LOGOFF` bindings through executor dispatch with
fake handlers; this checks every door name and each registered RUN target
without side effects. PDMATRIX and sponsor-menu choices use dedicated dispatch
paths with their own tests. `//` and `~~` auto-run entries are dispatched
before menu input and are covered by auto-run tests. The two empty-key LOGIN
defaults are automatic login-sequence steps rather than selectable actions,
and are tested through the login flow. These route checks prove dispatch; they
do not prove every command's user-visible outcome. Handler behavior still
needs meaningful assertions, especially where one handler serves several
different options. Treat the inventory counts as linked coverage, and review
each row's tests for the intended outcome.

## Live journey coverage

The live journeys log in, so they update the caller's login counters and
last-caller data. The main terminal suite also saves the selected
message-header style if the account has not chosen one yet. The HELLO example
door increments its `v3.data` visit counter; the oneliner, voting, and
auto-message prompts are cancelled. The journeys cover:

- Login and arrival at the main menu.
- All three fast-login routes to the main, transfer, and message menus.
- The page-to-node prompt and its blank-input cancel path.
- The empty voting-booth create prompt declined, and local chat join/quit.
- User stats, system stats, version, user directory, news, online callers, the
  empty oneliner view with add declined, read-only user settings, and
  auto-signature cancellation.
- Message menu, message-area listing and next/previous navigation, header-style
  picker, message-area and conference picker cancellation, next/previous conference navigation, blank-title compose
  abort, empty message list and reader, auto-signature view, and QWK menu navigation.
- Private Mail send cancellation and empty read/list states, file-area picker,
  transfer menu, file prompt cancellation, search/newscan and upload cancellation,
  empty batch/list states, file column and newscan-area settings, all seven
  shipped example doors, rumor-add cancellation and empty rumor
  newscan/search/delete/random paths, rumors, BBS directory
  listing and add cancellation plus empty edit/delete/verify states, InfoForms,
  and SysOp menu navigation.
- SysOp command aliases, the SysOp-to-V3Net route, and file-to-message-menu
  navigation. Read-only SysOp journeys also check the validation queue, user
  editor, disabled user-purge state, and news listing screens.
- Logoff confirmation and cancellation.

GitHub Actions runs these live journeys in a separate `terminal-regression`
job. It provisions a fresh board under the runner's temporary directory,
binds its listeners to loopback, connects with the seeded disposable sysop,
and stops the server even when the test fails. This job runs on pull requests
and pushes to `main`; it does not need credentials for a shared board.

`TestLocalRejectedPasswordRegression` is opt-in because an incorrect password
increments the BBS failed-login counter and repeated runs can trigger the
configured IP lockout. Enable it with `BBS_REGRESSION_TEST_BAD_PASSWORD=1`.

## Start a disposable board

Use the development setup to create an isolated instance. It seeds the sysop
account `Felonius` with password `password` and starts Telnet on port 2323:

```sh
./dev-setup.sh /private/tmp/vision3-regression --symlink
cd /private/tmp/vision3-regression
./vision3
```

Run the complete live suite from a second terminal in the repository:

```sh
BBS_REGRESSION_HOST=127.0.0.1 \
BBS_REGRESSION_PORT=2323 \
BBS_REGRESSION_HANDLE=Felonius \
BBS_REGRESSION_PASSWORD=password \
go test -tags bbsregression ./internal/bbsregression -run '^TestLocal' -v
```

With no `BBS_REGRESSION_*` variables, live tests skip while the scripted MCP
and terminal unit tests still run. For a non-disposable account, provide the
password through a secret environment mechanism rather than shell history.

SSH login is a separate opt-in journey; provide a host, port, account, and
pinned host-key fingerprint alongside the same BBS handle and password:

```sh
BBS_REGRESSION_SSH_HOST=127.0.0.1 \
BBS_REGRESSION_SSH_PORT=2222 \
BBS_REGRESSION_SSH_USER=bbs \
BBS_REGRESSION_SSH_HOST_KEY_SHA256=SHA256:... \
BBS_REGRESSION_HANDLE=Felonius \
BBS_REGRESSION_PASSWORD=password \
go test -tags bbsregression ./internal/bbsregression -run '^TestLocalSSHLoginRegression$' -v
```

## Run unit and scripted MCP tests

```sh
go test ./internal/bbsregression/... ./cmd/bbsregress-mcp
```

The fake Telnet peer checks connection, screen, key send, regex wait, timeout,
idle wait, invalid requests, one-session-at-a-time behavior, and disconnect.
The `keys` and `vt` packages cover key-token/encoding behavior and terminal
screen rendering, including CP437, UTF-8, cursor movement, and split ANSI
sequences.

## Use the MCP interactively

Start the stdio server against the disposable board:

```sh
go run ./cmd/bbsregress-mcp --host 127.0.0.1 --port 2323 --protocol telnet
```

The server exposes `connect`, `screen`, `send`, `wait`, and `disconnect`. It
allows one live terminal session at a time. `send` can change board data, so
keep interactive work on a disposable instance. For SSH, provide the account
and pinned SHA256 host-key fingerprint; unknown host keys are rejected.
