#!/bin/sh

# ---------------------------------------------------------------------------
# Privilege drop
#
# Bind-mounted volumes arrive owned by the host uid -- root, when Docker itself
# created the directory. A build-time chown cannot reach them, so the container
# starts as root, fixes ownership of the mounts, and re-execs this script as
# vision3. Everything below therefore runs unprivileged.
#
# If the operator pinned a uid (docker run --user), we are not root and cannot
# chown; carry on and let the writes succeed or fail on their own merits.
# ---------------------------------------------------------------------------
if [ "$(id -u)" = "0" ]; then
    mkdir -p /vision3/configs /vision3/data /vision3/menus /vision3/temp /vision3/bin
    # Walk the tree only when the mount root is not already ours. Docker creates
    # these owned by root on a fresh install, which is the case worth repairing;
    # on every restart afterwards a recursive pass would traverse the whole of
    # data/ -- every file area, every message base -- to change nothing.
    owner_uid=$(id -u vision3)
    for d in /vision3/configs /vision3/data /vision3/temp /vision3/bin; do
        if [ "$(stat -c %u "$d" 2>/dev/null)" != "$owner_uid" ]; then
            chown -R vision3:vision3 "$d" 2>/dev/null
        fi
    done
    # menus/ is deliberately NOT chowned. docker-compose.yml bind-mounts the
    # repository checkout there, and taking ownership of it leaves the host user
    # unable to `git pull` or edit their own menu set -- the container silently
    # breaking the working tree it was started from. The BBS only reads menus,
    # and a normal checkout is already world-readable.
    #
    # The trade-off: menuedit cannot save into a bind-mounted menus/ unless the
    # host makes it writable by uid 100. Editing on the host, or dropping the
    # mount to use the set baked into the image, both avoid that.
    exec su-exec vision3 "$0" "$@"
fi

# Create necessary directories
mkdir -p /vision3/configs
mkdir -p /vision3/data/users
mkdir -p /vision3/data/logs
mkdir -p /vision3/data/msgbases/privmail
mkdir -p /vision3/data/files/general
mkdir -p /vision3/data/infoforms/templates
mkdir -p /vision3/data/infoforms/responses
mkdir -p /vision3/temp
for d in \
    /vision3/data/ftn/in \
    /vision3/data/ftn/secure_in \
    /vision3/data/ftn/temp_in \
    /vision3/data/ftn/temp_out \
    /vision3/data/ftn/out \
    /vision3/data/ftn/dupehist \
    /vision3/data/ftn/dloads \
    /vision3/data/ftn/dloads/pass
do
    mkdir -p "$d"
done

# Note: data/ftn/binkd.conf is intentionally NOT created here. The FTN Setup
# Wizard (in the BBS config editor) generates a fully-configured binkd.conf;
# pre-seeding the raw template left placeholder values the mailer refuses.

# Create per-node temp directories (used by doors and session temp files)
for i in $(seq 1 10); do
    mkdir -p /vision3/temp/node${i}
done

# Generate SSH host keys if missing
if [ ! -f "/vision3/configs/ssh_host_rsa_key" ]; then
    echo "No RSA host key found, generating one..."
    ssh-keygen -t rsa -f /vision3/configs/ssh_host_rsa_key -N "" -q
fi
if [ ! -f "/vision3/configs/ssh_host_ed25519_key" ]; then
    echo "No ED25519 host key found, generating one..."
    ssh-keygen -t ed25519 -f /vision3/configs/ssh_host_ed25519_key -N "" -q
fi

# Copy any missing template configs (runs on every start to pick up newly added
# files). The .txt pass covers the IP blocklist/allowlist that config.json's
# ipBlocklistPath / ipAllowlistPath point at -- keep both globs in step with
# setup.sh, which seeds the same files for a native install.
for template_file in /vision3/templates/configs/*.json /vision3/templates/configs/*.txt; do
    [ -f "$template_file" ] || continue
    target="/vision3/configs/$(basename "$template_file")"
    if [ ! -f "$target" ]; then
        echo "  Creating $(basename "$target") from template..."
        cp "$template_file" "$target"
    fi
done

# Copy infoform templates and config if they don't exist
if [ -f "/vision3/templates/infoforms/config.json" ] && [ ! -f "/vision3/data/infoforms/config.json" ]; then
    echo "  Creating infoforms config.json from template..."
    cp /vision3/templates/infoforms/config.json /vision3/data/infoforms/config.json
fi
for template_file in /vision3/templates/infoforms/form_*.txt; do
    if [ -f "$template_file" ]; then
        target="/vision3/data/infoforms/templates/$(basename "$template_file")"
        if [ ! -f "$target" ]; then
            echo "  Creating $(basename "$template_file") from template..."
            cp "$template_file" "$target"
        fi
    fi
done

# Seed the initial data files setup.sh writes for a native install. users.json is
# deliberately absent: the BBS creates the default sysop account itself on first
# start (internal/user/manager.go), so writing it here would fork a second copy
# of those defaults.
[ -f /vision3/data/oneliners.json ]        || echo "[]" > /vision3/data/oneliners.json
[ -f /vision3/data/users/callhistory.json ] || echo "[]" > /vision3/data/users/callhistory.json
[ -f /vision3/data/users/callnumber.json ]  || echo "1"  > /vision3/data/users/callnumber.json

# Ensure sexyz.ini is in bin/ (binary must be provided by user)
mkdir -p /vision3/bin
if [ ! -f "/vision3/bin/sexyz.ini" ] && [ -f "/vision3/templates/configs/sexyz.ini" ]; then
    echo "Copying sexyz.ini to bin/..."
    cp /vision3/templates/configs/sexyz.ini /vision3/bin/sexyz.ini
fi

# Initialise the JAM message bases, as setup.sh does. Harmless once they exist,
# and non-fatal if it fails -- the message manager opens bases on demand -- but
# say so rather than swallowing it, or the first symptom is an empty message area.
if [ -x /vision3/v3mail ]; then
    if ! /vision3/v3mail stats --all --config /vision3/configs --data /vision3/data >/dev/null; then
        echo "WARNING: could not initialise JAM message bases; message areas may be unavailable" >&2
    fi
fi

exec "$@"
