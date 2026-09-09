#!/bin/bash
# build-binkd.sh — Build a binkd binary whose CRAM-MD5 actually works
#
# binkd's md5b.h picks the 32-bit integer type for MD5 from SIZEOF_INT, which
# only its configure script defines. Compiled without configure, the fallback
# `typedef unsigned long int UINT4` makes every MD5 word 64 bits wide on any
# LP64 platform (x86-64 Linux included), and MD5 then produces wrong digests.
# Plaintext binkp passwords still work, so the damage shows up only as
# CRAM-MD5 sessions failing in both directions with a correct password —
# see issue #268.
#
# This script runs the documented build (mkfls/unix + configure + make) and
# then verifies the MD5 in the binary it just produced against the published
# RFC 2202 HMAC-MD5 vector, so a broken binary can never be shipped silently.
#
# Usage:
#   ./scripts/build-binkd.sh [--src <dir>] [--out <path>] [--keep]
#
# Examples:
#   ./scripts/build-binkd.sh                       # clone, build, leave binkd in ./build-binkd/
#   ./scripts/build-binkd.sh --out /opt/v3/bin/binkd
#   ./scripts/build-binkd.sh --src ~/src/binkd     # build an existing checkout

set -euo pipefail

BINKD_REPO="https://github.com/pgul/binkd"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}==>${NC} $*"; }
fail() { echo -e "${RED}ERROR:${NC} $*" >&2; exit 1; }

# ── Parse arguments ──────────────────────────────────────────────
SRC=""
OUT=""
KEEP=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --src)  SRC="${2:-}"; shift 2 ;;
        --out)  OUT="${2:-}"; shift 2 ;;
        --keep) KEEP=1; shift ;;
        --help|-h)
            sed -n '2,23p' "$0" | sed 's/^# \?//'
            exit 0
            ;;
        *) fail "unknown argument: $1 (try --help)" ;;
    esac
done

for tool in gcc make; do
    command -v "$tool" >/dev/null || fail "$tool is required but not installed"
done

# ── Obtain the sources ───────────────────────────────────────────
WORKDIR=""
if [[ -n "$SRC" ]]; then
    [[ -f "$SRC/binkd.c" ]] || fail "$SRC does not look like a binkd checkout (no binkd.c)"
    WORKDIR="$(cd "$SRC" && pwd)"
    info "Building the checkout at $WORKDIR"
else
    command -v git >/dev/null || fail "git is required to fetch the sources (or pass --src)"
    WORKDIR="$(pwd)/build-binkd"
    if [[ -d "$WORKDIR" ]]; then
        [[ $KEEP -eq 1 ]] || rm -rf "$WORKDIR"
    fi
    if [[ ! -d "$WORKDIR" ]]; then
        info "Cloning $BINKD_REPO"
        git clone --depth 1 "$BINKD_REPO" "$WORKDIR"
    fi
fi

cd "$WORKDIR"

# ── Configure ────────────────────────────────────────────────────
# The unix build files live in mkfls/unix and must be copied to the source
# root before configure will run — this is the step whose omission produces
# the broken-MD5 binary described above.
info "Installing the unix build files and running configure"
cp mkfls/unix/* .
./configure >/dev/null || fail "configure failed"

grep -q -- '-DSIZEOF_INT=4' Makefile ||
    fail "configure did not define SIZEOF_INT — MD5 would be built with 64-bit words"

# binkd's sources use unprototyped K&R declarations, which GCC 15 rejects
# under its C23 default. Pin the dialect the code was written for.
DIALECT="-std=gnu17"
info "Compiling (CFLAGS += $DIALECT)"
make CFLAGS="-Wall -Wno-char-subscripts -O2 -g $DIALECT" >/dev/null || fail "make failed"

[[ -x ./binkd ]] || fail "make finished but produced no binkd binary"

# ── Verify the MD5 that was actually compiled in ─────────────────
# Rebuild md5b.c with the same defines the binary used and check hmac_md5
# against RFC 2202 test case 1: a key of twenty 0x0b bytes over "Hi There".
info "Verifying CRAM-MD5 against the RFC 2202 test vector"
DEFINES="$(sed -n 's/^AUTODEFS=//p' Makefile)"
cat > md5-selftest.c <<'EOF'
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* hmac_md5 is static inside md5b.c, so pull the whole translation unit in
   rather than linking against it. */
#include "md5b.c"

/* md5b.c reaches for a few globals owned by the rest of binkd. The MD5 and
   HMAC routines never touch them; these stand-ins just satisfy the linker. */
int mypid;
int ext_rand;
void *xalloc(size_t size) { return malloc(size); }

int main(void) {
    unsigned char key[16], digest[16];
    int i;

    memset(key, 0x0b, sizeof key);
    hmac_md5((unsigned char *)"Hi There", 8, key, sizeof key, digest);
    for (i = 0; i < 16; i++)
        printf("%02x", digest[i]);
    printf("\n");
    return 0;
}
EOF
# shellcheck disable=SC2086  # DEFINES is a pre-quoted list of -D flags
eval gcc $DIALECT -w $DEFINES -DHAVE_FORK -DUNIX '-DOS=\"UNIX\"' -I. \
    -o md5-selftest md5-selftest.c || fail "could not build the MD5 self-test"

EXPECTED="9294727a3638bb1c13f48ef8158bfc9d"
ACTUAL="$(./md5-selftest)"
rm -f md5-selftest md5-selftest.c

if [[ "$ACTUAL" != "$EXPECTED" ]]; then
    fail "this build's HMAC-MD5 is wrong (got $ACTUAL, want $EXPECTED).
       CRAM-MD5 sessions would fail in both directions. Do not ship this binary."
fi
info "MD5 self-test passed"

# ── Install ──────────────────────────────────────────────────────
./binkd -vv | head -1

if [[ -n "$OUT" ]]; then
    if [[ -e "$OUT" ]]; then
        BACKUP="$OUT.bak-$(date +%Y%m%d-%H%M%S)"
        warn "Backing up the existing binary to $BACKUP"
        cp -p "$OUT" "$BACKUP"
    fi
    mkdir -p "$(dirname "$OUT")"
    install -m 0755 ./binkd "$OUT"
    info "Installed $OUT"
    echo ""
    echo "Restart the BBS so the supervisor picks up the new binary."
else
    info "Built $WORKDIR/binkd"
    echo ""
    echo "Copy it into your instance with:"
    echo "    cp $WORKDIR/binkd /path/to/vision3/bin/binkd"
fi
