#!/bin/sh
# Run the image's application from the writable data volume. The image binary
# stays immutable so a later Docker image update can establish a new baseline.
set -eu

image_binary=$1
shift
runtime_dir=${POOL_APP_UPDATE_DIR:-/app/data/runtime}
runtime_binary=$runtime_dir/sub2api
base_file=$runtime_dir/image.sha256
pending_file=$runtime_dir/update-pending
rollback_file=$runtime_dir/update-rolled-back
backup_binary=$runtime_dir/sub2api.backup
export POOL_APP_UPDATE_DIR=$runtime_dir
unset POOL_APP_UPDATE_SUPERVISED
umask 077
mkdir -p "$runtime_dir"

hash_file() { sha256sum "$1" | cut -d ' ' -f 1; }
replace_binary() {
    cp "$1" "$runtime_binary.installing" || return 1
    chmod 700 "$runtime_binary.installing" || return 1
    mv -f "$runtime_binary.installing" "$runtime_binary"
}

image_hash=$(hash_file "$image_binary")
base_hash=$(cat "$base_file" 2>/dev/null || true)
if [ "$image_hash" != "$base_hash" ] || [ ! -x "$runtime_binary" ]; then
    replace_binary "$image_binary"
    # Preserve the job history. The application reconciles an interrupted job
    # against its current version when a Docker update changes the baseline.
    rm -f "$pending_file" "$rollback_file" "$backup_binary" "$runtime_dir/update-recovery-required"
    printf '%s\n' "$image_hash" > "$base_file.tmp"
    mv -f "$base_file.tmp" "$base_file"
fi

# CLI commands must neither start a health watcher nor confirm a pending update.
for argument in "$@"; do
    case "$argument" in
        -version|--version|-version=*|--version=*|-licenses|--licenses|-licenses=*|--licenses=*|-h|--help|-help|-setup|--setup|-setup=*|--setup=*)
            exec "$runtime_binary" "$@"
            ;;
    esac
done

export POOL_APP_UPDATE_SUPERVISED=1
if [ ! -f "$pending_file" ]; then
    exec "$runtime_binary" "$@"
fi

pending_hash=$(cat "$pending_file")
runtime_hash=$(hash_file "$runtime_binary")
if [ "$pending_hash" != "$runtime_hash" ]; then
    # The container stopped after the marker was persisted but before the atomic
    # binary swap. Do not mistake the old application's health for update success.
    printf '%s\n' 'Update interrupted before the new program was installed.' > "$rollback_file.tmp"
    mv -f "$rollback_file.tmp" "$rollback_file"
    rm -f "$pending_file"
    exec "$runtime_binary" "$@"
fi

health_timeout=${POOL_APP_UPDATE_HEALTH_TIMEOUT_SECONDS:-120}
case "$health_timeout" in ''|*[!0-9]*) health_timeout=120 ;; esac
if [ "$health_timeout" -lt 1 ]; then health_timeout=120; fi
health_url="http://127.0.0.1:${SERVER_PORT:-8080}"
confirmed_file=$runtime_dir/.startup-confirmed.$$
rm -f "$confirmed_file"
child_pid=
watcher_pid=
stop() {
    # A user-requested container stop keeps pending state for the next start.
    trap '' TERM INT
    if [ -n "$watcher_pid" ]; then kill "$watcher_pid" 2>/dev/null || true; fi
    if [ -n "$child_pid" ]; then
        kill -"$1" "$child_pid" 2>/dev/null || true
        wait "$child_pid" 2>/dev/null || true
    fi
    rm -f "$confirmed_file"
    case "$1" in INT) exit 130 ;; *) exit 143 ;; esac
}
trap 'stop TERM' TERM
trap 'stop INT' INT

"$runtime_binary" "$@" &
child_pid=$!
(
    deadline=$(( $(date +%s) + health_timeout ))
    successes=0
    while kill -0 "$child_pid" 2>/dev/null; do
        if wget -q -T 2 -O /dev/null "$health_url/health" ||
            wget -q -T 2 -O /dev/null "$health_url/setup/status"; then
            successes=$((successes + 1))
            # Require two observations of the live child, including a delay, so
            # an immediately crashing process cannot confirm its own update.
            if [ "$successes" -ge 2 ] && kill -0 "$child_pid" 2>/dev/null; then
                printf '%s\n' "$pending_hash" > "$confirmed_file"
                rm -f "$rollback_file" "$pending_file"
                exit 0
            fi
        else
            successes=0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
            kill -TERM "$child_pid" 2>/dev/null || true
            # Bound recovery even if a broken program ignores SIGTERM.
            sleep 5
            kill -KILL "$child_pid" 2>/dev/null || true
            exit 1
        fi
        sleep 1
    done
) &
watcher_pid=$!
child_status=0
wait "$child_pid" || child_status=$?
kill "$watcher_pid" 2>/dev/null || true
wait "$watcher_pid" 2>/dev/null || true
child_pid=
watcher_pid=
trap - TERM INT

if [ -s "$confirmed_file" ]; then
    # A later online update can already have created its own pending marker by
    # the time this healthy process exits. Leave that next update untouched.
    rm -f "$confirmed_file"
    exit "$child_status"
fi
rm -f "$confirmed_file"

if [ ! -x "$backup_binary" ]; then
    printf '%s\n' 'Updated program failed to start; no executable backup is available.' > "$rollback_file.tmp"
    mv -f "$rollback_file.tmp" "$rollback_file"
    cp "$rollback_file" "$runtime_dir/update-recovery-required"
    echo 'Pool update recovery failed: no executable backup.' >&2
    exit 1
fi

if ! replace_binary "$backup_binary"; then
    printf '%s\n' 'Restoring the previous application failed; inspect runtime files before retrying.' > "$runtime_dir/update-recovery-required"
    echo 'Pool update recovery failed: could not restore the previous program.' >&2
    exit 1
fi
printf '%s\n' 'Updated program failed its startup health check; restored the previous program.' > "$rollback_file.tmp"
mv -f "$rollback_file.tmp" "$rollback_file"
rm -f "$pending_file"
echo 'Pool update startup failed; starting the previous program.' >&2
exec "$runtime_binary" "$@"
