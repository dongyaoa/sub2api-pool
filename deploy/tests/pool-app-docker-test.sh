#!/usr/bin/env bash
# Exercise the actual image entrypoint, BusyBox shell, HTTP setup server and
# process recovery. The isolated container has no network or production mounts.
set -euo pipefail
image=${1:?pass the locally built pool image}
nonce="$(date +%s)-$$-$RANDOM"
container="pool-app-runtime-test-$nonce"
volume="pool-app-runtime-test-$nonce"
volume_created=false
container_created=false
cleanup() {
  status=$?
  if [[ $container_created == true ]]; then
    if [[ $status -ne 0 ]]; then docker logs "$container" >&2 || true; fi
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  if [[ $volume_created == true ]]; then
    docker volume rm "$volume" >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

image_id=$(docker image inspect "$image" --format '{{.Id}}')
docker volume create --label "sub2api.pool.runtime-test=$nonce" "$volume" >/dev/null
volume_created=true
container_created=true
docker run -d --name "$container" --pull never --network none --restart no \
  --mount "type=volume,src=$volume,dst=/app/data" \
  -e SERVER_HOST=127.0.0.1 -e SERVER_PORT=8080 \
  -e POOL_APP_UPDATE_HEALTH_TIMEOUT_SECONDS=10 "$image_id" >/dev/null

ready() {
  [[ $(docker inspect "$container" --format '{{.State.Running}}') == true ]] || return 1
  docker exec "$container" wget -q -T 2 -O - http://127.0.0.1:8080/setup/status \
    | jq -e '.data.needs_setup == true' >/dev/null
}
wait_ready() {
  for attempt in {1..40}; do
    if ready 2>/dev/null; then return 0; fi
    sleep 1
  done
  echo 'Pool runtime did not serve its real setup endpoint.' >&2
  return 1
}
wait_marker_cleared() {
  for attempt in {1..40}; do
    if docker exec "$container" sh -c 'test ! -e "$POOL_APP_UPDATE_DIR/update-pending"' 2>/dev/null; then
      wait_ready
      return
    fi
    sleep 1
  done
  echo 'Pool runtime did not finish pending startup verification.' >&2
  return 1
}

wait_ready
docker exec "$container" wget -q -T 2 -O - http://127.0.0.1:8080/ \
  | grep -i '<!doctype html' >/dev/null
docker exec --user sub2api "$container" sh -eu -c '
  printf persistent-fixture > /app/data/runtime-fixture-data
  cmp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api"
  cp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api.backup"
  chmod 700 "$POOL_APP_UPDATE_DIR/sub2api.backup"
  printf previous-failure > "$POOL_APP_UPDATE_DIR/update-rolled-back"
  sha256sum "$POOL_APP_UPDATE_DIR/sub2api" | cut -d " " -f 1 > "$POOL_APP_UPDATE_DIR/update-pending"
'

# The same real program acts as a valid downloaded application. Container restart
# ensures only the new process can answer the health check; there is no old server.
docker restart --time 10 "$container" >/dev/null
wait_marker_cleared
docker exec --user sub2api "$container" sh -eu -c '
  test ! -e "$POOL_APP_UPDATE_DIR/update-rolled-back"
  test ! -e "$POOL_APP_UPDATE_DIR/update-recovery-required"
  test "$(cat /app/data/runtime-fixture-data)" = persistent-fixture
  cmp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api"
  cp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api.backup"
  chmod 700 "$POOL_APP_UPDATE_DIR/sub2api.backup"
  printf "#!/bin/sh\nexit 1\n" > "$POOL_APP_UPDATE_DIR/.fixture-next"
  chmod 700 "$POOL_APP_UPDATE_DIR/.fixture-next"
  mv -f "$POOL_APP_UPDATE_DIR/.fixture-next" "$POOL_APP_UPDATE_DIR/sub2api"
  sha256sum "$POOL_APP_UPDATE_DIR/sub2api" | cut -d " " -f 1 > "$POOL_APP_UPDATE_DIR/update-pending"
'

# A matched pending checksum for a broken executable must restore the real backup
# and successfully serve HTTP within this same container (restart policy is off).
docker restart --time 10 "$container" >/dev/null
wait_marker_cleared
docker exec --user sub2api "$container" sh -eu -c '
  test -s "$POOL_APP_UPDATE_DIR/update-rolled-back"
  test ! -e "$POOL_APP_UPDATE_DIR/update-recovery-required"
  cmp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api"
  cmp /app/sub2api "$POOL_APP_UPDATE_DIR/sub2api.backup"
  test "$(cat /app/data/runtime-fixture-data)" = persistent-fixture
'
test "$(docker inspect "$container" --format '{{.Image}}')" = "$image_id"
printf '%s\n' 'Real Docker application startup, pending verification, crash recovery and persistent data checks passed.'
