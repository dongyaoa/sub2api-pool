#!/usr/bin/env python3
"""Offline deployment identity tests; credentials are deliberate fixture strings."""
import copy
import importlib.util
import os
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

MODULE = Path(__file__).resolve().parents[1] / "pool-updater-config.py"
SPEC = importlib.util.spec_from_file_location("pool_config", MODULE)
CONFIG = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CONFIG)


class ConfigTest(unittest.TestCase):
    def setUp(self):
        self.container = {
            "Config": {
                "Labels": {
                    "com.docker.compose.project": "original-project",
                    "com.docker.compose.service": "sub2api",
                    "com.docker.compose.project.working_dir": "/www/wwwroot/sub2api",
                    "com.docker.compose.project.config_files": "/www/wwwroot/sub2api/docker-compose.yml,/www/wwwroot/sub2api/production.yml",
                    "com.docker.compose.project.environment_file": "/etc/sub2api/base.env,/etc/sub2api/production.env",
                },
                "Env": ["PATH=/bin", "DATABASE_PASSWORD=fixture-only", "SERVER_PORT=8080"],
            },
            "State": {"Running": True},
            "HostConfig": {"PortBindings": {"8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "9080"}]}},
            "Mounts": [{"Type": "volume", "Name": "existing-data", "Destination": "/app/data", "RW": True}],
        }
        self.config = CONFIG.derive(self.container, "/usr/bin/docker", ["/usr/bin/docker", "compose"])
        self.image = {"Config": {"Env": ["PATH=/bin"]}}
        self.rendered = {"services": {"sub2api": {
            "environment": {"DATABASE_PASSWORD": "fixture-only", "SERVER_PORT": "8080"},
            "ports": [{"target": 8080, "published": "9080", "host_ip": "127.0.0.1"}],
            "volumes": [{"type": "volume", "source": "data", "target": "/app/data"}],
        }}, "volumes": {"data": {"name": "existing-data"}}}

    def test_keeps_original_project_all_files_and_environment_files(self):
        self.assertEqual(self.config["project_name"], "original-project")
        self.assertEqual(self.config["compose_files"], ["/www/wwwroot/sub2api/docker-compose.yml", "/www/wwwroot/sub2api/production.yml"])
        self.assertEqual(self.config["env_files"], ["/etc/sub2api/base.env", "/etc/sub2api/production.env"])
        CONFIG.verify_identity(self.config, self.container)
        CONFIG.verify_runtime(self.config, self.container, self.rendered, self.image)

    def test_does_not_infer_missing_compose_identity(self):
        for label in ["com.docker.compose.project", "com.docker.compose.project.working_dir", "com.docker.compose.project.config_files", "com.docker.compose.service"]:
            container = copy.deepcopy(self.container)
            del container["Config"]["Labels"][label]
            with self.assertRaises(ValueError):
                CONFIG.derive(container, "/usr/bin/docker", ["/usr/bin/docker", "compose"])

    def test_dotenv_used_only_from_recorded_working_directory(self):
        del self.container["Config"]["Labels"]["com.docker.compose.project.environment_file"]
        with patch.object(os.path, "isfile", side_effect=lambda p: p == "/www/wwwroot/sub2api/.env"):
            config = CONFIG.derive(self.container, "/usr/bin/docker", ["/usr/bin/docker", "compose"])
        self.assertEqual(config["env_files"], ["/www/wwwroot/sub2api/.env"])

    def test_environment_drift_fails_without_exposing_values(self):
        self.rendered["services"]["sub2api"]["environment"]["DATABASE_PASSWORD"] = "different-private-fixture"
        with self.assertRaises(ValueError) as captured:
            CONFIG.verify_runtime(self.config, self.container, self.rendered, self.image)
        self.assertNotIn("different-private-fixture", str(captured.exception))
        self.assertNotIn("fixture-only", str(captured.exception))

    def test_port_and_volume_drift_fail(self):
        for mutation in [lambda r: r["services"]["sub2api"]["ports"][0].update(published="8080"), lambda r: r["volumes"]["data"].update(name="new-data")]:
            rendered = copy.deepcopy(self.rendered)
            mutation(rendered)
            with self.assertRaises(ValueError):
                CONFIG.verify_runtime(self.config, self.container, rendered, self.image)

    def test_existing_managed_overlay_is_not_captured_as_base(self):
        self.container["Config"]["Labels"]["com.docker.compose.project.config_files"] += ",/var/lib/sub2api-pool-updater/compose.override.json"
        self.container["Config"]["Env"].append("POOL_UPDATER_SOCKET=/run/sub2api-pool-updater/updater.sock")
        self.container["Mounts"].append({"Type": "bind", "Source": "/run/sub2api-pool-updater", "Destination": "/run/sub2api-pool-updater", "RW": False})
        config = CONFIG.derive(self.container, "/usr/bin/docker", ["/usr/bin/docker", "compose"])
        self.assertEqual(config["compose_files"], self.config["compose_files"])
        CONFIG.verify_identity(config, self.container)
        CONFIG.verify_runtime(config, self.container, self.rendered, self.image)

    def test_same_file_names_in_another_order_are_rejected(self):
        self.config["compose_files"].reverse()
        with self.assertRaises(ValueError):
            CONFIG.verify_identity(self.config, self.container)

    def test_compose_v1_is_rejected_before_recreation(self):
        with patch.object(CONFIG.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b"legacy help", stderr=b"")):
            with self.assertRaisesRegex(ValueError, "Compose v2"):
                CONFIG.verify_compose_capabilities(self.config)

    def test_compose_capability_checks_use_clean_environment(self):
        with patch.object(CONFIG.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b"--pull --format", stderr=b"")) as run:
            CONFIG.verify_compose_capabilities(self.config)
        self.assertEqual(run.call_count, 2)
        for call in run.call_args_list:
            self.assertEqual(call.kwargs["env"], CONFIG.ENVIRONMENT)

    def test_unknown_config_fields_and_relative_paths_fail(self):
        for mutate in [lambda c: c.update(image="untrusted/image"), lambda c: c.update(socket_path="relative.sock"), lambda c: c.update(compose_command=["sh", "-c", "docker compose"])]:
            config = copy.deepcopy(self.config)
            mutate(config)
            with self.assertRaises(ValueError):
                CONFIG.validate(config)

    def test_anonymous_volume_is_not_silently_recreated(self):
        self.rendered["services"]["sub2api"]["volumes"][0]["source"] = ""
        with self.assertRaisesRegex(ValueError, "anonymous"):
            CONFIG.verify_runtime(self.config, self.container, self.rendered, self.image)

    @unittest.skipUnless(os.name == "posix", "Unix ownership and permission semantics")
    def test_symlinks_and_writable_files_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.json"
            target.write_text("{}", encoding="utf-8")
            target.chmod(0o666)
            with self.assertRaises(ValueError):
                CONFIG.trusted_file(str(target))
            target.chmod(0o600)
            link = Path(directory) / "link.json"
            link.symlink_to(target)
            with self.assertRaises(ValueError):
                CONFIG.trusted_file(str(link))


if __name__ == "__main__":
    unittest.main()
