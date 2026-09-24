#!/usr/bin/env bash
# Install only the fixed Pool repository's verified Linux amd64 host helper.
set -euo pipefail
umask 077

repository=ghcr.io/dongyaoa/sub2api-pool
container=sub2api
config_source=
compose_bin=
docker_bin=/usr/bin/docker
config_path=/etc/sub2api-pool-updater/config.json
helper_path=/usr/local/libexec/sub2api-pool-updater
service=sub2api-pool-updater.service

fail() { printf 'Pool updater installation: %s\n' "$1" >&2; exit 1; }
usage() {
  printf '%s\n' 'Usage: sudo bash install-pool-updater.sh [--container NAME_OR_ID] [--config /absolute/config.json] [--compose-bin /absolute/docker-compose]'
}
while (($#)); do
  case "$1" in
    --container|--config|--compose-bin)
      (($# >= 2)) || fail "missing argument for $1"
      case "$1" in
        --container) container=$2 ;;
        --config) config_source=$2 ;;
        --compose-bin) compose_bin=$2 ;;
      esac
      shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) usage >&2; fail 'unsupported argument' ;;
  esac
done

[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || fail 'requires a Linux amd64 host'
[[ $EUID -eq 0 ]] || fail 'run this installer with sudo/root'
[[ $container =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || fail 'invalid container name or ID'
[[ -x $docker_bin ]] || fail 'Docker must be installed at /usr/bin/docker'
[[ -d /run/systemd/system ]] || fail 'a running systemd host is required'
for command in python3 systemctl install mktemp; do command -v "$command" >/dev/null || fail "missing prerequisite: $command"; done
if systemctl is-active --quiet "$service"; then
  fail 'the updater is already running; check its job status, wait for completion, then stop the service before reinstalling'
fi
if [[ -z $config_source && -f $config_path ]]; then config_source=$config_path; fi
[[ -z $config_source || $config_source == /* ]] || fail '--config must be an absolute trusted JSON file'
[[ -z $compose_bin || $compose_bin == /* ]] || fail '--compose-bin must be absolute'
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES
temporary=$(mktemp -d /var/tmp/sub2api-pool-installer.XXXXXX)
extraction=
cleanup() {
  if [[ -n $extraction ]]; then "$docker_bin" rm "$extraction" >/dev/null 2>&1 || true; fi
  rm -rf -- "$temporary"
}
trap cleanup EXIT

printf '%s\n' 'Pulling and checking the fixed Pool image repository...'
"$docker_bin" pull "$repository:latest"
"$docker_bin" image inspect "$repository:latest" > "$temporary/latest.json"
python3 - "$temporary/latest.json" "$temporary/identity" <<'PY'
import json, re, sys
with open(sys.argv[1], encoding="utf-8") as f:
    image, = json.load(f)
labels = image.get("Config", {}).get("Labels") or {}
version = labels.get("org.opencontainers.image.version", "")
revision = labels.get("org.opencontainers.image.revision", "")
digests = [d for d in image.get("RepoDigests", []) if re.fullmatch(r"ghcr\.io/dongyaoa/sub2api-pool@sha256:[a-f0-9]{64}", d)]
if (image.get("Os") != "linux" or image.get("Architecture") != "amd64" or
    labels.get("org.opencontainers.image.source") != "https://github.com/dongyaoa/sub2api-pool" or
    not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)-pool\.(?:0|[1-9][0-9]*)", version) or
    not re.fullmatch(r"[a-f0-9]{40}", revision) or len(digests) != 1 or
    not re.fullmatch(r"sha256:[a-f0-9]{64}", image.get("Id", ""))):
    raise SystemExit("Pool image metadata/platform verification failed")
with open(sys.argv[2], "w", encoding="utf-8") as f:
    f.write(version + "\n" + digests[0] + "\n" + image["Id"] + "\n")
PY
mapfile -t identity < "$temporary/identity"
version=${identity[0]}
image=${identity[1]}
image_id=${identity[2]}
"$docker_bin" pull "$repository:$version"
"$docker_bin" image inspect "$repository:$version" > "$temporary/version.json"
python3 - "$temporary/version.json" "$image" "$image_id" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    image, = json.load(f)
if image.get("Id") != sys.argv[3] or sys.argv[2] not in image.get("RepoDigests", []):
    raise SystemExit("latest and version tags do not identify the same published image; retry after publication completes")
PY

extraction=$("$docker_bin" create --network none "$image_id")
"$docker_bin" cp "$extraction:/app/pool-updater" "$temporary/pool-updater" || fail 'this image predates the host updater; wait for a release containing the feature'
"$docker_bin" cp "$extraction:/app/pool-updater-install" "$temporary/assets" || fail 'verified image lacks installer assets'
"$docker_bin" container inspect "$container" > "$temporary/container.json"
"$docker_bin" exec "$container" test -x /app/pool-updater || fail 'first update sub2api once from the terminal to a Pool release containing /app/pool-updater; bootstrap never upgrades the current image'
arguments=(--inspect-file "$temporary/container.json" --output "$temporary/config.json" --docker "$docker_bin")
if [[ -n $config_source ]]; then arguments+=(--config "$config_source"); fi
if [[ -n $compose_bin ]]; then arguments+=(--compose-bin "$compose_bin"); fi
python3 "$temporary/assets/pool-updater-config.py" "${arguments[@]}"
python3 - "$temporary/assets/pool-updater-config.py" <<'PY'
import importlib.util, os, sys
spec = importlib.util.spec_from_file_location("pool_config", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
for directory in ["/usr/local/libexec", "/usr/local/sbin", "/etc/sub2api-pool-updater", "/var/lib/sub2api-pool-updater", "/run/sub2api-pool-updater", "/etc/systemd/system"]:
    while not os.path.lexists(directory):
        directory = os.path.dirname(directory)
    module.trusted_directory(directory)
for path in ["/usr/local/libexec/sub2api-pool-updater", "/usr/local/sbin/sub2api-pool-compose", "/etc/sub2api-pool-updater/config.json", "/etc/systemd/system/sub2api-pool-updater.service"]:
    if os.path.lexists(path):
        module.trusted_file(path)
PY

# Nothing has touched the running service until all identity/config checks pass.
install -d -o root -g root -m 0755 /usr/local/libexec /usr/local/sbin
install -d -o root -g root -m 0700 /etc/sub2api-pool-updater /var/lib/sub2api-pool-updater
install -o root -g root -m 0755 "$temporary/pool-updater" "$helper_path"
install -o root -g root -m 0755 "$temporary/assets/pool-updater-compose.py" /usr/local/sbin/sub2api-pool-compose
install -o root -g root -m 0600 "$temporary/config.json" "$config_path"
install -o root -g root -m 0644 "$temporary/assets/pool-updater.service" "/etc/systemd/system/$service"
printf '%s\n' 'Adding the private updater socket to the current application image; this briefly recreates only sub2api...'
"$helper_path" --config "$config_path" --bootstrap || fail 'bootstrap did not pass health verification; check the host journal and restore the deployment before starting the updater'
systemctl daemon-reload
systemctl enable --now "$service"
systemctl is-active --quiet "$service" || fail 'the host updater did not start'
printf '%s\n' 'Host updater installed. Refresh the administrator page.' 'For terminal management use: sudo sub2api-pool-compose up -d' 'The original Compose files, ports and data volumes were preserved; keep the managed overlay for future commands.'
