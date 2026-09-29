#!/bin/sh
# Executable fixtures exercise the real supervisor, atomic file transitions and
# signals. wget is deterministic here; pool-images also checks real HTTP in Docker.
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/pool-app-runtime.XXXXXX")
supervisor=
cleanup() {
    if [ -n "$supervisor" ]; then
        kill -TERM "$supervisor" 2>/dev/null || true
        wait "$supervisor" 2>/dev/null || true
    fi
    rm -rf "$fixture"
}
trap cleanup EXIT HUP INT TERM
fail() { echo "pool app runtime test failed: $*" >&2; exit 1; }
mkdir -p "$fixture/bin" "$fixture/state"
export TEST_STATE=$fixture/state
export POOL_APP_UPDATE_DIR=$fixture/runtime
export POOL_APP_UPDATE_HEALTH_TIMEOUT_SECONDS=2
export PATH=$fixture/bin:$PATH
runtime=$POOL_APP_UPDATE_DIR/sub2api
pending=$POOL_APP_UPDATE_DIR/update-pending
rollback=$POOL_APP_UPDATE_DIR/update-rolled-back

cat > "$fixture/bin/wget" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$TEST_STATE/health-requests"
version=$(cat "$TEST_STATE/active" 2>/dev/null || true)
case "$version" in
    unready) exit 1 ;;
    setup) case "$*" in */setup/status) exit 0 ;; *) exit 1 ;; esac ;;
    *) exit 0 ;;
esac
SH
chmod +x "$fixture/bin/wget"

make_binary() {
    destination=$1
    version=$2
    printf '#!/bin/sh\nversion=%s\n' "$version" > "$destination"
    cat >> "$destination" <<'SH'
for argument in "$@"; do
    case "$argument" in
        --version|-version|--help|-h|--setup) printf '%s:%s:%s\n' "$version" "${POOL_APP_UPDATE_SUPERVISED:-no}" "$*"; exit 0 ;;
    esac
done
if [ "$version" = broken ]; then exit 17; fi
printf '%s\n' "$version" > "$TEST_STATE/active"
printf '%s\n' "$$" > "$TEST_STATE/pid.$version"
printf '%s\n' "$*" > "$TEST_STATE/args.$version"
trap 'printf "%s\n" "$version" > "$TEST_STATE/stopped"; exit 0' TERM INT
while :; do sleep 1; done
SH
    chmod +x "$destination"
}
run_cli() { sh "$repo_root/deploy/pool-app-runtime.sh" "$fixture/image" "$@"; }
stage() {
    cp "$runtime" "$POOL_APP_UPDATE_DIR/sub2api.backup"
    make_binary "$fixture/new" "$1"
    mv -f "$fixture/new" "$runtime"
    sha256sum "$runtime" | cut -d ' ' -f 1 > "$pending"
}
start() {
    rm -f "$TEST_STATE/active" "$TEST_STATE/stopped"
    sh "$repo_root/deploy/pool-app-runtime.sh" "$fixture/image" "$@" > "$fixture/output" 2>&1 &
    supervisor=$!
}
stop() {
    kill -TERM "$supervisor" 2>/dev/null || true
    wait "$supervisor" 2>/dev/null || true
    supervisor=
}
wait_for() {
    tries=0
    while ! "$@"; do
        tries=$((tries + 1))
        if [ "$tries" -ge 100 ]; then cat "$fixture/output" >&2; fail "timed out: $*"; fi
        sleep 0.1
    done
}
active_is() { [ "$(cat "$TEST_STATE/active" 2>/dev/null || true)" = "$1" ]; }
missing() { [ ! -e "$1" ]; }

make_binary "$fixture/image" base
[ "$(run_cli --version)" = 'base:no:--version' ] || fail 'bootstrap CLI or supervision flag'
make_binary "$runtime" online
[ "$(run_cli --version)" = 'online:no:--version' ] || fail 'same image lost online update'
make_binary "$fixture/image" image2
[ "$(run_cli --version)" = 'image2:no:--version' ] || fail 'new image did not replace persisted binary'

stage healthy
[ "$(run_cli --version)" = 'healthy:no:--version' ] || fail 'CLI used wrong binary'
[ -f "$pending" ] || fail 'CLI confirmed update'
[ ! -f "$TEST_STATE/health-requests" ] || fail 'CLI ran health checks'
start --config fixture.yaml
wait_for missing "$pending"
kill -0 "$supervisor" || fail 'healthy process exited'
[ "$(cat "$TEST_STATE/args.healthy")" = '--config fixture.yaml' ] || fail 'server arguments lost'

# A second update exits the first supervised process. Its new pending marker must
# survive; the old supervisor must not restore the first update's backup.
stage healthy2
kill -TERM "$(cat "$TEST_STATE/pid.healthy")"
wait "$supervisor"
supervisor=
[ -f "$pending" ] || fail 'first supervisor consumed next update'
[ "$(run_cli --version)" = 'healthy2:no:--version' ] || fail 'second update rolled back incorrectly'
start
wait_for missing "$pending"
stop

stage unready
start
wait_for active_is healthy2
wait_for test -f "$rollback"
[ ! -e "$pending" ] || fail 'failed update kept pending after rollback'
stop

stage broken
start
wait_for test -f "$rollback"
wait_for active_is healthy2
stop
[ "$(run_cli --version)" = 'healthy2:no:--version' ] || fail 'immediate crash was not restored'

# Persisted marker but no swap: old process must not masquerade as update success.
printf '%064d\n' 0 > "$pending"
rm -f "$TEST_STATE/health-requests"
start
wait_for missing "$pending"
[ -f "$rollback" ] || fail 'interrupted install did not report failure'
[ ! -f "$TEST_STATE/health-requests" ] || fail 'interrupted install checked old health'
stop

stage setup
start
wait_for missing "$pending"
[ ! -f "$rollback" ] || fail 'successful update kept stale rollback marker'
stop

stage unready
start
wait_for active_is unready
stop
[ -f "$pending" ] || fail 'container stop lost pending recovery state'
[ "$(cat "$TEST_STATE/stopped")" = unready ] || fail 'SIGTERM was not forwarded'

stage broken
rm -f "$POOL_APP_UPDATE_DIR/sub2api.backup"
start
if wait "$supervisor"; then fail 'missing backup returned success'; fi
supervisor=
[ -f "$POOL_APP_UPDATE_DIR/update-recovery-required" ] || fail 'missing backup did not lock recovery'

make_binary "$fixture/image" image3
[ "$(run_cli --version)" = 'image3:no:--version' ] || fail 'image update failed with pending state'
[ ! -e "$pending" ] && [ ! -e "$rollback" ] || fail 'image baseline retained stale update state'
[ ! -e "$POOL_APP_UPDATE_DIR/update-recovery-required" ] || fail 'new image retained stale recovery lock'
printf '%s\n' 'pool app runtime tests passed'
