# QWK/REP test fixtures

Binary packet samples used by `internal/qwk` (and higher-level service) tests to
guard against format regressions and to grow toward Synchronet/MultiMail
compatibility coverage.

## Layout

- `vision3/` — packets produced by ViSiON/3 itself
  - `VISION3.QWK` — a small mail packet (CONTROL.DAT, MESSAGES.DAT, NDX, etc.)
  - `VISION3.REP` — a reply packet (`VISION3.MSG` inside the zip)
- `malformed/` — intentionally broken packets that must fail gracefully
  - `TRUNCATED.REP` — a valid zip whose `.MSG` is shorter than one 128-byte block
- `external/` — packets produced by third-party readers/BBSes (Synchronet,
  MultiMail, etc.). Empty for now; drop real-world samples here as they become
  available so import behavior can be validated against them.

## Regenerating ViSiON/3 fixtures

The ViSiON/3-generated fixtures were produced by a guarded generator test. If the
baseline packet format changes intentionally, regenerate them by temporarily
reinstating a generator (see git history of `rep_writer_test.go` / fixture tests)
and running it with `GEN_QWK_FIXTURES=1`. External fixtures are never regenerated
— they are captured from real readers on purpose.
