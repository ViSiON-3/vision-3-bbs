# Security Policy

ViSiON/3 is BBS software that sysops run on the public Internet, so we take
security reports seriously and appreciate the time it takes to send one.

## Reporting a Vulnerability

**Please do not report security vulnerabilities in public GitHub issues,
pull requests, discussions or on Discord.** A public report gives attackers a
head start on every board that has not yet been patched.

Report privately through GitHub instead:

1. Go to the [Security tab](https://github.com/ViSiON-3/vision-3-bbs/security)
   of this repository.
2. Click **Report a vulnerability**, or open the
   [private advisory form](https://github.com/ViSiON-3/vision-3-bbs/security/advisories/new)
   directly.
3. Fill in the form and submit it.

The report is visible only to you and the project maintainers.

If you do not have a GitHub account, email **spaceman@vision3bbs.com** with
"SECURITY" in the subject line. Email is not encrypted, so send a short note
first if the details are sensitive and we will arrange how to share them.

### What to Include

The more of this you can give us, the faster we can confirm and fix the
problem:

- The ViSiON/3 version (shown at the top of `vision3 doctor`), your
  operating system and architecture, and whether you run a release build,
  Docker or a build from source.
- Which component is affected: the BBS server (SSH or telnet), the `wfc`
  sysop console, V3Net, FTN or QWK networking, the QWK API, doors or
  scripting, file transfers, or one of the bundled tools.
- Steps to reproduce, or a proof of concept.
- What an attacker gains: for example login bypass, privilege escalation,
  access to another user's mail, remote code execution or denial of service.
- Any configuration needed to trigger it, especially non-default settings.
- Relevant logs or packet captures, with passwords, keys and caller details
  removed.

Please do not test against a board you do not run. Reproduce the issue on
your own installation.

## What to Expect

ViSiON/3 is maintained by volunteers, so these are goals rather than
guarantees:

- We aim to acknowledge your report within **7 days**.
- We will tell you whether we can reproduce it and how we rate its severity,
  and keep you updated as the fix progresses.
- Once a fix is released we publish a GitHub security advisory and note the
  fix in the release notes. We will credit you in the advisory unless you
  ask us not to.

We ask that you give us a reasonable chance to release a fix before
disclosing the issue publicly. If you have a disclosure deadline, tell us in
the report so we can work to it.

## Supported Versions

ViSiON/3 is pre-1.0 and moves quickly. Security fixes land in the next
release; older releases are not patched.

| Version                                                                        | Supported |
| ------------------------------------------------------------------------------ | --------- |
| [Latest release](https://github.com/ViSiON-3/vision-3-bbs/releases/latest)     | ✅        |
| Older releases                                                                 | ❌        |

If you run a board, upgrading to the latest release is the way to receive
security fixes.

## Scope

In scope is the code in this repository and the binaries we build from it.

Out of scope for this policy:

- **Third-party binaries we ship but do not build**, such as `sexyz` and
  `binkd`. Report those to their upstream projects. Do tell us if the way
  ViSiON/3 invokes or configures them creates the problem.
- **Vulnerabilities in a Go dependency** that ViSiON/3 does not expose.
  Report those upstream; let us know if we need to bump the version.
- **Problems on a specific board** caused by that sysop's configuration,
  doors or scripts. Contact that board's sysop.

## Hardening Your Board

This policy covers flaws in the software. For help configuring a board
securely (connection limits, bot defenses, IP filtering, login lockout and
access levels), see the
[Security Guide](docs/sysop/configuration/security.md). Questions about
configuration are welcome in GitHub issues and on
[Discord](https://discord.gg/trZ9VSsF3r).
