"""Check startup ordering without starting containers or opening Desktop."""

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[1]
STUB = """#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

name = Path(sys.argv[0]).name
with open(os.environ["DOKJA_START_TEST_LOG"], "a", encoding="utf-8") as log:
    log.write(json.dumps([name, *sys.argv[1:]]) + "\\n")
if (name == "compose" and "restart" in sys.argv
        and os.environ.get("DOKJA_START_TEST_FAIL_RESTART")):
    sys.exit(12)
"""


@unittest.skipUnless(shutil.which("make"), "make is required")
class StartupTests(unittest.TestCase):
    def run_target(self, target, fail_restart=False):
        with tempfile.TemporaryDirectory(prefix="dokja-start-test-") as directory:
            root = Path(directory)
            shutil.copyfile(REPO / "Makefile", root / "Makefile")
            (root / "dokja_interfaces/desktop/node_modules").mkdir(parents=True)
            for name in ("compose", "pnpm"):
                script = root / name
                script.write_text(STUB, encoding="utf-8")
                script.chmod(0o755)
            log = root / "calls.jsonl"
            environment = dict(os.environ, DOKJA_START_TEST_LOG=str(log))
            # Do not inherit recursive-make flags or an unrelated test's controls.
            for key in (
                "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "DOKJA_START_TEST_FAIL_RESTART"
            ):
                environment.pop(key, None)
            if fail_restart:
                environment["DOKJA_START_TEST_FAIL_RESTART"] = "1"
            result = subprocess.run(
                [
                    "make", "--no-print-directory", "-j4", target,
                    "COMPOSE=./compose", "PNPM=./pnpm", "ENV_FILE=local.env",
                    "STACK_FILE=local stack.yml", "SERVICES=dokja-feeds dokja-discord",
                ],
                cwd=root,
                env=environment,
                capture_output=True,
                text=True,
                timeout=15,
            )
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            return result, calls, root

    def expected_up(self):
        prefix = [
            "compose", "--env-file", "local.env", "-f", "local stack.yml",
            "-f", "docker-compose.postgres.yml",
        ]
        return prefix, [
            prefix + ["up", "-d", "dokja-postgres"],
            prefix + [
                "exec", "-T", "dokja-postgres", "pg_isready", "-U", "dokja",
                "-d", "dokja",
            ],
            prefix + ["up", "-d", "dokja-feeds", "dokja-discord"],
        ]

    def test_start_restarts_selected_services_before_desktop_even_with_parallel_make(self):
        result, calls, root = self.run_target("start")
        self.assertEqual(result.returncode, 0, result.stderr)
        prefix, expected = self.expected_up()
        expected += [
            prefix + ["restart", "dokja-feeds", "dokja-discord"],
            [
                "pnpm", "--dir", "dokja_interfaces/desktop", "start",
                f"--env-file={root / 'local.env'}",
            ],
        ]
        self.assertEqual(calls, expected)

    def test_up_does_not_restart_running_services_or_open_desktop(self):
        result, calls, _ = self.run_target("up")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, self.expected_up()[1])

    def test_failed_restart_prevents_desktop_startup(self):
        result, calls, _ = self.run_target("start", fail_restart=True)
        self.assertNotEqual(result.returncode, 0)
        prefix, expected = self.expected_up()
        self.assertEqual(
            calls, expected + [prefix + ["restart", "dokja-feeds", "dokja-discord"]]
        )


if __name__ == "__main__":
    unittest.main()
