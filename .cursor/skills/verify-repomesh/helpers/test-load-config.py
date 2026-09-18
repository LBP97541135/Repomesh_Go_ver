#!/usr/bin/env python3
"""Checks for helpers/load-config.py. Run from the repository root."""

from __future__ import annotations

import os
import subprocess
import tempfile
from pathlib import Path

HELPERS = Path(__file__).resolve().parent
LOADER = HELPERS / "load-config.py"
EXAMPLE = HELPERS.parent / "config.example.yaml"


def run(args: list[str], *, env: dict[str, str] | None = None, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    return subprocess.run(
        ["python3", str(LOADER), *args],
        text=True,
        capture_output=True,
        env=merged,
        cwd=str(cwd or Path.cwd()),
    )


def write_yaml(dir_path: Path, name: str, body: str) -> Path:
    path = dir_path / name
    path.write_text(body, encoding="utf-8")
    return path


def main() -> int:
    failures: list[str] = []

    example = run(["--check", "--config", str(EXAMPLE)])
    if example.returncode != 0 or "origin=http://127.0.0.1:18080" not in example.stdout:
        failures.append(f"example check failed: {example.returncode} {example.stdout} {example.stderr}")
    if "using example" not in example.stderr:
        failures.append("example should warn that it is the committed template")

    exported = run(["--export", "--config", str(EXAMPLE)])
    if exported.returncode != 0 or 'REPOMESH_VERIFY_ORIGIN="http://127.0.0.1:18080"' not in exported.stdout:
        failures.append(f"example export failed: {exported.stdout} {exported.stderr}")
    if "https://repomesh.example.com" in exported.stdout:
        failures.append("export must not default to a public hostname")

    public = run(["--config", str(EXAMPLE), "--json"])
    if public.returncode != 0 or '"https://repomesh.example.com"' in public.stdout:
        failures.append(f"example json leaked a public hostname: {public.stdout}")

    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp)
        public_origin = write_yaml(
            tmp_path,
            "public.yaml",
            "\n".join(
                [
                    "listen:",
                    '  host: "repomesh.example.com"',
                    "  port: 443",
                    'origin: "https://repomesh.example.com"',
                    'run_scope: "unconfigured-only"',
                    "confirmations: {}",
                    "database: {}",
                ]
            ),
        )
        rejected = run(["--check", "--config", str(public_origin)])
        if rejected.returncode == 0 or "not local" not in rejected.stderr and "must be 127.0.0.1" not in rejected.stderr:
            failures.append(f"public hostname should fail: {rejected.stderr}")

        live_empty = write_yaml(
            tmp_path,
            "live.yaml",
            "\n".join(
                [
                    "listen:",
                    '  host: "127.0.0.1"',
                    "  port: 18080",
                    'run_scope: "account-a-live"',
                    "confirmations:",
                    "  holder_will_click_github_ui: false",
                    "database:",
                    '  url_env_var: "REPOMESH_DATABASE_URL"',
                ]
            ),
        )
        live = run(["--check", "--config", str(live_empty)])
        if live.returncode == 0 or "auth_config_path" not in live.stderr:
            failures.append(f"live scope should require auth_config_path: {live.stderr}")

        secret = tmp_path / "auth.json"
        secret.write_text("{}\n", encoding="utf-8")
        url_file = tmp_path / "connection-url.txt"
        url_file.write_text("postgres://example.invalid/repomesh\n", encoding="utf-8")
        live_ok = write_yaml(
            tmp_path,
            "live-ok.yaml",
            "\n".join(
                [
                    "listen:",
                    '  host: "127.0.0.1"',
                    "  port: 18080",
                    'run_scope: "account-a-live"',
                    f'auth_config_path: "{secret}"',
                    "reuse_existing_postgres_and_wrap_root: true",
                    'github_account_a_login: "catbobyman"',
                    "installed_private_repo_id: 1367444901",
                    "confirmations:",
                    "  holder_will_click_github_ui: true",
                    "database:",
                    '  url_env_var: ""',
                    f'  connection_url_file: "{url_file}"',
                ]
            ),
        )
        live_check = run(["--check", "--config", str(live_ok)])
        if live_check.returncode != 0:
            failures.append(f"filled live yaml should pass: {live_check.stderr}")
        live_export = run(["--export", "--config", str(live_ok)])
        if live_export.returncode != 0:
            failures.append(f"filled live export failed: {live_export.stderr}")
        if "postgres://example.invalid/repomesh" not in live_export.stdout:
            failures.append("live export should read the connection-url file")
        if "catbobyman" not in live_export.stdout:
            failures.append("live export should include the GitHub login")

        restore = write_yaml(
            tmp_path,
            "restore.yaml",
            live_ok.read_text(encoding="utf-8").replace("account-a-live", "restore-leftovers"),
        )
        restore_check = run(["--check", "--config", str(restore)])
        if restore_check.returncode == 0 or "restore-leftovers requires true confirmations" not in restore_check.stderr:
            failures.append(f"restore should require leftover flags: {restore_check.stderr}")

    if failures:
        print("load-config tests failed:")
        for item in failures:
            print(f"- {item}")
        return 1
    print("load-config tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
