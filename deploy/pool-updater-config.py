#!/usr/bin/env python3
"""Derive a fixed host configuration; reject deployment drift without printing secrets."""
import argparse
import json
import os
import posixpath
import re
import stat
import subprocess
import sys

REPOSITORY = "ghcr.io/dongyaoa/sub2api-pool"
SOURCE = "https://github.com/dongyaoa/sub2api-pool"
ENVIRONMENT = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root", "DOCKER_HOST": "unix:///var/run/docker.sock"}


def trusted_file(path):
    if not isinstance(path, str) or not os.path.isabs(path) or os.path.normpath(path) != path or any(c in path for c in "\r\n\0"):
        raise ValueError("deployment paths must be clean absolute paths")
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise ValueError("deployment files must be root-owned regular files without group/other write access")
    trusted_directory(os.path.dirname(path))


def trusted_directory(path):
    while True:
        info = os.lstat(path)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("deployment directories and their parents must be root-owned without group/other write access or symlinks")
        parent = os.path.dirname(path)
        if parent == path:
            return
        path = parent


def validate(config):
    keys = {"compose_files", "project_name", "working_dir", "env_files", "socket_path", "state_dir", "health_timeout_seconds", "socket_gid", "docker_path", "compose_command"}
    if set(config) - keys or not keys.issubset(config):
        raise ValueError("config must provide exactly the documented fields")
    if not isinstance(config["project_name"], str) or not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,62}", config["project_name"]):
        raise ValueError("invalid Compose project name")
    if not isinstance(config["compose_files"], list) or not 1 <= len(config["compose_files"]) <= 16 or not isinstance(config["env_files"], list) or len(config["env_files"]) > 16:
        raise ValueError("invalid Compose or environment file list")
    command = config["compose_command"]
    if not isinstance(command, list) or len(command) not in (1, 2) or (len(command) == 2 and command != [config["docker_path"], "compose"]):
        raise ValueError("compose_command must be an absolute executable or docker compose")
    for path in [config["working_dir"], config["socket_path"], config["state_dir"]]:
        if not isinstance(path, str) or not os.path.isabs(path) or os.path.normpath(path) != path or any(c in path for c in "\r\n\0"):
            raise ValueError("configuration paths must be clean absolute paths")
    if config["socket_path"] != "/run/sub2api-pool-updater/updater.sock" or config["state_dir"] != "/var/lib/sub2api-pool-updater":
        raise ValueError("installer requires the documented dedicated socket and state directories")
    if type(config["socket_gid"]) is not int or config["socket_gid"] < 0 or type(config["health_timeout_seconds"]) is not int or not 10 <= config["health_timeout_seconds"] <= 900:
        raise ValueError("invalid updater limits")
    for path in config["compose_files"] + config["env_files"] + [config["docker_path"], command[0]]:
        trusted_file(path)
    if not os.path.isdir(config["working_dir"]):
        raise ValueError("Compose working directory does not exist")
    trusted_directory(config["working_dir"])


def derive(container, docker, command):
    labels = container.get("Config", {}).get("Labels") or {}
    if labels.get("com.docker.compose.service") != "sub2api" or labels.get("com.docker.compose.oneoff", "false").lower() == "true":
        raise ValueError("selected container must be the existing Compose sub2api service")
    project = labels.get("com.docker.compose.project", "")
    directory = labels.get("com.docker.compose.project.working_dir", "")
    files = labels.get("com.docker.compose.project.config_files", "").split(",")
    files = [path for path in files if path != "/var/lib/sub2api-pool-updater/compose.override.json"]
    if not project or not directory or not files or any(not path for path in files):
        raise ValueError("complete Compose project, working_dir and config_files labels are required; supply a trusted --config")
    env_label = labels.get("com.docker.compose.project.environment_file", "")
    env_files = env_label.split(",") if env_label else []
    if not env_files and os.path.isfile(posixpath.join(directory, ".env")):
        env_files = [posixpath.join(directory, ".env")]
    return {"compose_files": files, "project_name": project, "working_dir": directory, "env_files": env_files, "socket_path": "/run/sub2api-pool-updater/updater.sock", "state_dir": "/var/lib/sub2api-pool-updater", "health_timeout_seconds": 180, "socket_gid": 1000, "docker_path": docker, "compose_command": command}


def verify_identity(config, container):
    labels = container.get("Config", {}).get("Labels") or {}
    actual = labels.get("com.docker.compose.project.config_files", "").split(",")
    actual = [path for path in actual if path != posixpath.join(config["state_dir"], "compose.override.json")]
    if labels.get("com.docker.compose.service") != "sub2api" or labels.get("com.docker.compose.project") != config["project_name"] or labels.get("com.docker.compose.project.working_dir") != config["working_dir"] or actual != config["compose_files"]:
        raise ValueError("configuration does not exactly match the running Compose identity and ordered file list")
    if not container.get("State", {}).get("Running"):
        raise ValueError("the existing sub2api container must be running")


def env_map(values):
    return dict(value.split("=", 1) if "=" in value else (value, "") for value in values)


def verify_runtime(config, container, rendered, image):
    service = rendered.get("services", {}).get("sub2api")
    if not isinstance(service, dict):
        raise ValueError("Compose does not resolve a sub2api service")
    expected = env_map(image.get("Config", {}).get("Env") or [])
    expected.update({key: "" if value is None else str(value) for key, value in (service.get("environment") or {}).items()})
    actual = env_map(container.get("Config", {}).get("Env") or [])
    expected.pop("POOL_UPDATER_SOCKET", None)
    actual.pop("POOL_UPDATER_SOCKET", None)
    if expected != actual:
        raise ValueError("resolved service environment differs from the running container; persist original shell values in a root-owned env file and supply --config")
    bindings = []
    for port in service.get("ports") or []:
        bindings.append((str(port["target"]), port.get("protocol", "tcp"), port.get("host_ip") or "0.0.0.0", str(port.get("published", ""))))
    current = []
    for target, endpoints in (container.get("HostConfig", {}).get("PortBindings") or {}).items():
        number, protocol = target.split("/", 1)
        for endpoint in endpoints or []:
            current.append((number, protocol, endpoint.get("HostIp") or "0.0.0.0", endpoint.get("HostPort", "")))
    if sorted(bindings) != sorted(current):
        raise ValueError("resolved ports differ from the running container; refusing recreation")
    mounts = []
    for volume in service.get("volumes") or []:
        kind = volume.get("type")
        source = volume.get("source", "")
        if kind == "volume":
            if not source:
                raise ValueError("anonymous volumes cannot be safely reconstructed; declare a persistent named volume first")
            source = (rendered.get("volumes", {}).get(source) or {}).get("name", config["project_name"] + "_" + source)
        mounts.append((kind, source, volume["target"], bool(volume.get("read_only", False))))
    existing = [(item["Type"], item.get("Name", "") if item["Type"] == "volume" else item.get("Source", ""), item["Destination"], not item.get("RW", True)) for item in container.get("Mounts") or [] if item["Destination"] != "/run/sub2api-pool-updater"]
    if sorted(mounts) != sorted(existing):
        raise ValueError("resolved mounts differ from the running container; refusing recreation")


def run_json(command, directory):
    result = subprocess.run(command, cwd=directory, env=ENVIRONMENT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise ValueError("Docker/Compose validation failed; inspect the deployment privately for missing variables or configuration errors")
    return json.loads(result.stdout)


def verify_compose_capabilities(config):
    command = config["compose_command"]
    for suffix, required in [(["up", "--help"], b"--pull"), (["config", "--help"], b"--format")]:
        result = subprocess.run(command + suffix, cwd=config["working_dir"], env=ENVIRONMENT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
        if result.returncode or required not in result.stdout:
            raise ValueError("Compose v2 with 'up --pull' and 'config --format json' is required; legacy Python docker-compose v1 is unsupported")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inspect-file", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--docker", default="/usr/bin/docker")
    parser.add_argument("--compose-bin")
    parser.add_argument("--config")
    args = parser.parse_args()
    with open(args.inspect_file, encoding="utf-8") as stream:
        containers = json.load(stream)
    if len(containers) != 1:
        raise ValueError("exactly one existing container required")
    container = containers[0]
    if args.config:
        trusted_file(args.config)
        with open(args.config, encoding="utf-8") as stream:
            config = json.load(stream)
    else:
        command = [args.compose_bin] if args.compose_bin else [args.docker, "compose"]
        config = derive(container, args.docker, command)
    validate(config)
    verify_identity(config, container)
    verify_compose_capabilities(config)
    command = config["compose_command"] + ["--project-directory", config["working_dir"], "--project-name", config["project_name"]]
    for path in config["env_files"]:
        command += ["--env-file", path]
    for path in config["compose_files"]:
        command += ["-f", path]
    rendered = run_json(command + ["config", "--format", "json"], config["working_dir"])
    images = run_json([config["docker_path"], "image", "inspect", container["Image"]], config["working_dir"])
    if len(images) != 1:
        raise ValueError("current image identity is unavailable")
    verify_runtime(config, container, rendered, images[0])
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(config, stream, indent=2)
        stream.write("\n")
    print("Validated original Compose identity, environment, ports and persistent mounts.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, TypeError) as error:
        print("pool updater configuration: " + str(error), file=sys.stderr)
        sys.exit(1)
