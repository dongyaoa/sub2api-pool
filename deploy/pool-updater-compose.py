#!/usr/bin/env python3
"""Use the updater's original Compose project plus its managed image overlay."""
import json
import os
import stat
import sys

CONFIG = "/etc/sub2api-pool-updater/config.json"


def main():
    if os.geteuid() != 0:
        raise ValueError("run with sudo; the deployment configuration is root-owned")
    info = os.lstat(CONFIG)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise ValueError("untrusted updater configuration")
    with open(CONFIG, encoding="utf-8") as stream:
        config = json.load(stream)
    command = list(config["compose_command"])
    command += ["--project-directory", config["working_dir"], "--project-name", config["project_name"]]
    for path in config.get("env_files", []):
        command += ["--env-file", path]
    for path in config["compose_files"]:
        command += ["-f", path]
    overlay = os.path.join(config["state_dir"], "compose.override.json")
    if not os.path.isfile(overlay):
        raise ValueError("managed Compose overlay is missing; complete updater bootstrap first")
    command += ["-f", overlay]
    command += sys.argv[1:] or ["ps"]
    environment = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root", "DOCKER_HOST": "unix:///var/run/docker.sock"}
    os.chdir(config["working_dir"])
    os.execve(command[0], command, environment)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError) as error:
        print("pool compose: " + str(error), file=sys.stderr)
        sys.exit(1)
