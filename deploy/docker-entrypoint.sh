#!/bin/sh
set -e

# Fix data directory permissions when running as root.
# Docker named volumes / host bind-mounts may be owned by root,
# preventing the non-root sub2api user from writing files.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    # Use || true to avoid failure on read-only mounted files (e.g. config.yaml:ro)
    chown -R sub2api:sub2api /app/data 2>/dev/null || true
    # Re-invoke this script as sub2api so the flag-detection below
    # also runs under the correct user.
    exec su-exec sub2api "$0" "$@"
fi

# Compatibility: if the first arg looks like a flag (e.g. --help),
# prepend the default binary so it behaves the same as the old
# ENTRYPOINT ["/app/sub2api"] style.
if [ "$#" -eq 0 ]; then
    set -- /app/sub2api
elif [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi

# Only the default application uses the persistent, supervised runtime.
# Explicit commands (for example sh or pg_dump) retain normal Docker semantics.
if [ "$1" = /app/sub2api ]; then
    exec /app/pool-app-runtime.sh "$@"
fi

exec "$@"
