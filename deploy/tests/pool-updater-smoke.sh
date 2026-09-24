#!/usr/bin/env bash
# Real Docker/Compose bootstrap and restoration checks; no model/network calls.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo 'Run this isolated Docker smoke test as root.' >&2; exit 1; }
root=$(cd -- "$(dirname -- "$0")/../.." && pwd)
helper=${1:?pass an absolute Linux pool-updater binary}
image=${2:?pass the locally built, labelled pool image}
[[ $helper == /* && -x $helper ]] || { echo 'helper must be an executable absolute path' >&2; exit 1; }
docker compose version >/dev/null
base=$(mktemp -d /var/lib/sub2api-pool-smoke.XXXXXX)
project=pool-updater-smoke-$$
socket_dir=/run/$project
mkdir -p "$socket_dir" "$base/state"
chmod 0700 "$base" "$base/state"
chmod 0750 "$socket_dir"
cleanup() {
  docker compose --project-directory "$base" --project-name "$project" -f "$base/compose.json" down --volumes >/dev/null 2>&1 || true
  rm -rf -- "$base" "$socket_dir"
}
trap cleanup EXIT
image_id=$(docker image inspect "$image" --format '{{.Id}}')
export POOL_SMOKE_BASE=$base POOL_SMOKE_IMAGE=$image_id POOL_SMOKE_PROJECT=$project POOL_SMOKE_SOCKET=$socket_dir
python3 - <<'PY'
import json, os
base, image, project, socket = (os.environ[k] for k in ("POOL_SMOKE_BASE", "POOL_SMOKE_IMAGE", "POOL_SMOKE_PROJECT", "POOL_SMOKE_SOCKET"))
compose = {"services": {"sub2api": {"image": image, "user": "0:0", "entrypoint": ["/bin/sh", "-c", "exec sleep 3600"], "network_mode": "none", "environment": {"POOL_SMOKE_SETTING": "persisted-fixture"}, "volumes": ["state:/app/data"], "healthcheck": {"test": ["CMD", "/bin/sh", "-c", "test -f /app/sub2api"], "interval": "1s", "timeout": "1s", "retries": 1}}}, "volumes": {"state": {}}}
config = {"compose_files": [base + "/compose.json"], "project_name": project, "working_dir": base, "env_files": [], "socket_path": socket + "/updater.sock", "state_dir": base + "/state", "health_timeout_seconds": 10, "socket_gid": 1000, "docker_path": "/usr/bin/docker", "compose_command": ["/usr/bin/docker", "compose"]}
for name, data in [("compose.json", compose), ("config.json", config)]:
    with open(base + "/" + name, "w", encoding="utf-8") as f:
        json.dump(data, f)
    os.chmod(base + "/" + name, 0o600)
PY
compose=(docker compose --project-directory "$base" --project-name "$project" -f "$base/compose.json")
wait_healthy() {
  local id=$1
  for attempt in {1..30}; do
    if [[ $(docker inspect "$id" --format '{{.State.Health.Status}}') == healthy ]]; then return 0; fi
    sleep 1
  done
  echo 'Smoke fixture did not become healthy.' >&2
  return 1
}
"${compose[@]}" up -d --no-deps --pull never sub2api
id=$("${compose[@]}" ps -q sub2api)
wait_healthy "$id"
docker exec "$id" sh -c 'printf persistent-data > /app/data/pool-updater-smoke'
"$helper" --config "$base/config.json" --bootstrap
id=$("${compose[@]}" -f "$base/state/compose.override.json" ps -q sub2api)
test "$(docker inspect "$id" --format '{{.Image}}')" = "$image_id"
test "$(docker exec "$id" cat /app/data/pool-updater-smoke)" = persistent-data
test "$(docker exec "$id" printenv POOL_SMOKE_SETTING)" = persisted-fixture
docker inspect "$id" > "$base/current.json"
python3 - "$base" <<'PY'
import json, os, sys
base = sys.argv[1]
with open(base + "/current.json", encoding="utf-8") as f:
    container, = json.load(f)
assert any(m["Destination"] == "/run/sub2api-pool-updater" and not m["RW"] for m in container["Mounts"])
assert not any(m["Destination"] == "/var/run/docker.sock" for m in container["Mounts"])
with open(base + "/state/job.json", encoding="utf-8") as f:
    assert json.load(f)["job"]["state"] == "succeeded"
with open(base + "/compose.json", encoding="utf-8") as f:
    compose = json.load(f)
# Original deployment healthy; introducing the updater mount deliberately fails.
compose["services"]["sub2api"]["healthcheck"]["test"] = ["CMD", "/bin/sh", "-c", "test ! -d /run/sub2api-pool-updater"]
with open(base + "/compose.json", "w", encoding="utf-8") as f:
    json.dump(compose, f)
os.remove(base + "/state/compose.override.json")
PY
# Reset to the original declaration, then test the actual bootstrap rollback.
"${compose[@]}" up -d --no-deps --pull never sub2api
id=$("${compose[@]}" ps -q sub2api)
wait_healthy "$id"
if "$helper" --config "$base/config.json" --bootstrap; then
  echo 'Expected the intentionally unhealthy bootstrap to fail.' >&2
  exit 1
fi
id=$("${compose[@]}" -f "$base/state/compose.override.json" ps -q sub2api)
test "$(docker inspect "$id" --format '{{.Image}}')" = "$image_id"
test "$(docker inspect "$id" --format '{{.State.Health.Status}}')" = healthy
test "$(docker exec "$id" cat /app/data/pool-updater-smoke)" = persistent-data
python3 - "$base/state/job.json" "$base/state/compose.override.json" "$image_id" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    state = json.load(f)
assert state["job"]["state"] == "rolled_back"
assert not state.get("recovery_required", False)
with open(sys.argv[2], encoding="utf-8") as f:
    overlay = json.load(f)["services"]["sub2api"]
assert overlay["image"] == sys.argv[3]
assert not overlay.get("volumes")
PY
echo 'Real Docker bootstrap, health-failure restoration, image identity and data preservation passed.'
