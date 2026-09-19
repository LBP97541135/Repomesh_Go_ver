#!/usr/bin/env python3
"""Offline lifecycle guard checks; these are not Collector/AgentLoop integration tests."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ProcessOwnershipTests(unittest.TestCase):
    def invoke(self, script, action, state):
        return subprocess.run(
            ["bash", str(ROOT / "scripts" / script), action],
            env=dict(os.environ, REPOMESH_OBSERVE_STATE=str(state)),
            text=True,
            capture_output=True,
            timeout=15,
        )

    def test_empty_state_status_and_stop_are_safe(self):
        for script in ("observe-local.sh", "agentloop-local.sh"):
            with self.subTest(script=script), tempfile.TemporaryDirectory() as state:
                for action in ("status", "stop"):
                    result = self.invoke(script, action, state)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(json.loads(result.stdout)["status"], "not_running")

    def test_uninstalled_start_explains_requirement(self):
        for script in ("observe-local.sh", "agentloop-local.sh"):
            with self.subTest(script=script), tempfile.TemporaryDirectory() as state:
                result = self.invoke(script, "start", state)
                self.assertEqual(result.returncode, 1)
                self.assertIn("not installed", result.stderr)

    def test_stale_pid_marker_cannot_stop_unrelated_process(self):
        with subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"]) as sentinel:
            try:
                ticks = Path(f"/proc/{sentinel.pid}/stat").read_text().rsplit(") ", 1)[1].split()[19]
                for script, marker in (("observe-local.sh", "collector-process.json"),
                                       ("agentloop-local.sh", "agentloop-process.json")):
                    with self.subTest(script=script), tempfile.TemporaryDirectory() as state:
                        Path(state, marker).write_text(json.dumps({
                            "pid": sentinel.pid, "start_ticks": ticks,
                            "command": [sys.executable, "-c", "import time; time.sleep(60)"],
                            "config_file": str(Path(state, "config.yaml")),
                        }))
                        result = self.invoke(script, "stop", state)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertIn("no signal sent", json.loads(result.stdout)["detail"].lower())
                        self.assertIsNone(sentinel.poll())
            finally:
                sentinel.terminate()
                sentinel.wait(timeout=5)

    def test_shared_or_symlink_directory_is_rejected(self):
        for script in ("observe-local.sh", "agentloop-local.sh"):
            with self.subTest(script=script), tempfile.TemporaryDirectory() as base:
                shared = Path(base, "shared")
                shared.mkdir(mode=0o755)
                result = self.invoke(script, "status", shared)
                self.assertEqual(result.returncode, 1)
                link = Path(base, "link")
                link.symlink_to(shared, target_is_directory=True)
                result = self.invoke(script, "status", link)
                self.assertEqual(result.returncode, 1)


if __name__ == "__main__":
    unittest.main()
