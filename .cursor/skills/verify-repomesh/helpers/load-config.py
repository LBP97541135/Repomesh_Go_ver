#!/usr/bin/env python3
"""Load the verify-repomesh operator YAML and emit exports, JSON, or a check line."""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

try:
    import yaml
except ImportError as exc:  # pragma: no cover
    sys.stderr.write("python3 needs PyYAML to read the operator config\n")
    raise SystemExit(2) from exc

SCOPES = ("unconfigured-only", "account-a-live", "restore-leftovers")
LOCAL_HOSTS = {"127.0.0.1", "localhost", "::1"}
LIVE_SCOPES = {"account-a-live", "restore-leftovers"}
RESTORE_FLAGS = (
    "app_visibility_private_again",
    "installation_161284386_gone",
    "collaborator_and_invite_gone",
    "b_oauth_grant_revoked",
    "a_installation_161172403_selects_only_1367444901",
    "account_a_cannot_see_1368000734",
)


class ConfigError(Exception):
    pass


def skill_dir() -> Path:
    return Path(__file__).resolve().parent.parent


def default_config_paths() -> list[Path]:
    env = os.environ.get("REPOMESH_VERIFY_CONFIG", "").strip()
    paths: list[Path] = []
    if env:
        paths.append(Path(env))
    paths.append(skill_dir() / "config.yaml")
    paths.append(skill_dir() / "config.example.yaml")
    return paths


def resolve_config_path(explicit: str | None) -> tuple[Path, bool]:
    candidates = [Path(explicit)] if explicit else default_config_paths()
    seen: set[Path] = set()
    for raw in candidates:
        path = raw.expanduser()
        if not path.is_absolute():
            path = (Path.cwd() / path).resolve()
        else:
            path = path.resolve()
        if path in seen:
            continue
        seen.add(path)
        if path.is_file():
            using_example = path.name == "config.example.yaml"
            return path, using_example
    raise ConfigError(
        "missing operator config; copy the example with\n"
        "  cp .cursor/skills/verify-repomesh/config.example.yaml .cursor/skills/verify-repomesh/config.yaml"
    )


def as_dict(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ConfigError(f"{label} must be a mapping")
    return value


def as_str(value: Any, label: str, *, required: bool = False) -> str:
    if value is None:
        if required:
            raise ConfigError(f"{label} is required")
        return ""
    if not isinstance(value, str):
        raise ConfigError(f"{label} must be a string")
    return value.strip()


def as_bool(value: Any, label: str, *, required: bool = False) -> bool:
    if value is None:
        if required:
            raise ConfigError(f"{label} must be an explicit boolean")
        return False
    if not isinstance(value, bool):
        raise ConfigError(f"{label} must be a boolean")
    return value


def as_int(value: Any, label: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise ConfigError(f"{label} must be an integer")
    return value


def as_repo_id(value: Any, label: str) -> str:
    if value is None or value == "":
        return ""
    if isinstance(value, bool):
        raise ConfigError(f"{label} must be a numeric id")
    if isinstance(value, int):
        if value <= 0:
            raise ConfigError(f"{label} must be a positive numeric id")
        return str(value)
    if isinstance(value, str) and value.strip().isdigit():
        return value.strip()
    raise ConfigError(f"{label} must be a stable numeric id")


def require_abs_file(path_text: str, label: str) -> str:
    path = Path(path_text).expanduser()
    if not path.is_absolute():
        raise ConfigError(f"{label} must be an absolute path")
    if not path.is_file():
        raise ConfigError(f"{label} is not a readable file: {path}")
    return str(path)


def local_origin(host: str, port: int, origin_text: str) -> str:
    if origin_text:
        origin = origin_text.rstrip("/")
    else:
        origin = f"http://{host}:{port}"
    parsed = urlparse(origin)
    if parsed.scheme != "http":
        raise ConfigError(
            "skill drive origin must be http://127.0.0.1:<port> (or localhost). "
            "Live GitHub HTTPS belongs in auth.json, not this yaml"
        )
    if parsed.hostname not in LOCAL_HOSTS:
        raise ConfigError(
            f"skill drive origin host {parsed.hostname!r} is not local; "
            "use 127.0.0.1 or localhost, not a public hostname"
        )
    if parsed.path not in ("", "/") or parsed.query or parsed.fragment or parsed.username:
        raise ConfigError("skill drive origin must be a bare local origin")
    expected_port = parsed.port or 80
    if origin_text and expected_port != port:
        raise ConfigError("origin port must match listen.port")
    return f"{parsed.scheme}://{parsed.hostname}:{expected_port}"


def load_raw(path: Path) -> dict[str, Any]:
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
    except yaml.YAMLError as exc:
        raise ConfigError(f"invalid YAML in {path}: {exc}") from exc
    return as_dict(data, "config")


def validate(raw: dict[str, Any], *, using_example: bool, path: Path) -> dict[str, Any]:
    listen = as_dict(raw.get("listen") or {}, "listen")
    host = as_str(listen.get("host"), "listen.host") or "127.0.0.1"
    if host not in LOCAL_HOSTS:
        raise ConfigError("listen.host must be 127.0.0.1, localhost, or ::1")
    port = as_int(listen.get("port", 18080), "listen.port")
    if port < 1 or port > 65535:
        raise ConfigError("listen.port must be 1-65535")
    origin = local_origin(host, port, as_str(raw.get("origin"), "origin"))
    addr = f"{host}:{port}"

    scope = as_str(raw.get("run_scope"), "run_scope", required=True)
    if scope not in SCOPES:
        raise ConfigError(f"run_scope must be one of {', '.join(SCOPES)}")

    auth_config_path = as_str(raw.get("auth_config_path"), "auth_config_path")
    reuse = raw.get("reuse_existing_postgres_and_wrap_root")
    github_login = as_str(raw.get("github_account_a_login"), "github_account_a_login")
    installed_repo = as_repo_id(raw.get("installed_private_repo_id"), "installed_private_repo_id")
    out_of_install = as_repo_id(raw.get("out_of_install_repo_id"), "out_of_install_repo_id")
    confirmations = as_dict(raw.get("confirmations") or {}, "confirmations")
    holder = as_bool(confirmations.get("holder_will_click_github_ui"), "confirmations.holder_will_click_github_ui")
    database = as_dict(raw.get("database") or {}, "database")
    url_env_var = as_str(database.get("url_env_var"), "database.url_env_var") or "REPOMESH_DATABASE_URL"
    connection_url_file = as_str(database.get("connection_url_file"), "database.connection_url_file")

    if scope in LIVE_SCOPES:
        if using_example:
            raise ConfigError(
                f"{scope} cannot use config.example.yaml; copy it to config.yaml and fill the live fields"
            )
        if not auth_config_path:
            raise ConfigError("auth_config_path is required for live scopes")
        auth_config_path = require_abs_file(auth_config_path, "auth_config_path")
        if reuse is None:
            raise ConfigError("reuse_existing_postgres_and_wrap_root must be an explicit boolean for live scopes")
        reuse = as_bool(reuse, "reuse_existing_postgres_and_wrap_root", required=True)
        if not github_login:
            raise ConfigError("github_account_a_login is required for live scopes")
        if not holder:
            raise ConfigError("confirmations.holder_will_click_github_ui must be true for live scopes")
        if not installed_repo:
            raise ConfigError("installed_private_repo_id is required for live scopes")
        if not url_env_var and not connection_url_file:
            raise ConfigError("set database.url_env_var or database.connection_url_file")
        if connection_url_file:
            connection_url_file = require_abs_file(connection_url_file, "database.connection_url_file")
        elif url_env_var and not os.environ.get(url_env_var):
            raise ConfigError(f"environment variable {url_env_var} is not set")
    else:
        reuse = as_bool(reuse, "reuse_existing_postgres_and_wrap_root")
        if auth_config_path:
            auth_config_path = require_abs_file(auth_config_path, "auth_config_path")
        if connection_url_file:
            connection_url_file = require_abs_file(connection_url_file, "database.connection_url_file")

    restore_flags = {
        name: as_bool(confirmations.get(name), f"confirmations.{name}") for name in RESTORE_FLAGS
    }
    if scope == "restore-leftovers":
        missing = [name for name, value in restore_flags.items() if not value]
        if missing:
            raise ConfigError("restore-leftovers requires true confirmations: " + ", ".join(missing))

    mode = "live" if scope in LIVE_SCOPES else "unconfigured"
    return {
        "config_path": str(path),
        "using_example": using_example,
        "listen_host": host,
        "listen_port": port,
        "addr": addr,
        "origin": origin,
        "run_scope": scope,
        "mode": mode,
        "auth_config_path": auth_config_path,
        "reuse_existing_postgres_and_wrap_root": reuse,
        "github_account_a_login": github_login,
        "installed_private_repo_id": installed_repo,
        "out_of_install_repo_id": out_of_install,
        "holder_will_click_github_ui": holder,
        "restore_flags": restore_flags,
        "database_url_env_var": url_env_var,
        "database_connection_url_file": connection_url_file,
        "https_cookie_note": (
            "Local HTTP does not prove __Host- cookie live acceptance. "
            "account-a-live still needs the HTTPS origin inside auth.json."
        ),
    }


def database_url(cfg: dict[str, Any]) -> str:
    file_path = cfg["database_connection_url_file"]
    if file_path:
        return Path(file_path).read_text(encoding="utf-8").strip()
    env_name = cfg["database_url_env_var"]
    return os.environ.get(env_name, "")


def emit_export(cfg: dict[str, Any]) -> str:
    values = {
        "REPOMESH_VERIFY_CONFIG": cfg["config_path"],
        "REPOMESH_VERIFY_LISTEN_HOST": cfg["listen_host"],
        "REPOMESH_VERIFY_LISTEN_PORT": str(cfg["listen_port"]),
        "REPOMESH_VERIFY_ADDR": cfg["addr"],
        "REPOMESH_VERIFY_ORIGIN": cfg["origin"],
        "REPOMESH_VERIFY_RUN_SCOPE": cfg["run_scope"],
        "REPOMESH_VERIFY_MODE": cfg["mode"],
        "REPOMESH_VERIFY_GITHUB_LOGIN": cfg["github_account_a_login"],
        "REPOMESH_VERIFY_INSTALLED_REPO_ID": cfg["installed_private_repo_id"],
        "REPOMESH_VERIFY_OUT_OF_INSTALL_REPO_ID": cfg["out_of_install_repo_id"],
        "REPOMESH_VERIFY_REUSE_POSTGRES": "true" if cfg["reuse_existing_postgres_and_wrap_root"] else "false",
        "REPOMESH_VERIFY_HOLDER_CLICKS": "true" if cfg["holder_will_click_github_ui"] else "false",
        "REPOMESH_VERIFY_DATABASE_URL_ENV": cfg["database_url_env_var"],
    }
    if cfg["auth_config_path"]:
        values["REPOMESH_AUTH_CONFIG"] = cfg["auth_config_path"]
    if cfg["database_connection_url_file"]:
        values["REPOMESH_VERIFY_DATABASE_URL_FILE"] = cfg["database_connection_url_file"]
    url = database_url(cfg)
    if url and cfg["mode"] == "live":
        values["REPOMESH_DATABASE_URL"] = url
    lines = [f"export {key}={json.dumps(value)}" for key, value in values.items()]
    return "\n".join(lines) + "\n"


def public_json(cfg: dict[str, Any]) -> dict[str, Any]:
    return {
        "configPath": cfg["config_path"],
        "usingExample": cfg["using_example"],
        "addr": cfg["addr"],
        "origin": cfg["origin"],
        "runScope": cfg["run_scope"],
        "mode": cfg["mode"],
        "authConfigPathSet": bool(cfg["auth_config_path"]),
        "reuseExistingPostgresAndWrapRoot": cfg["reuse_existing_postgres_and_wrap_root"],
        "githubAccountALoginSet": bool(cfg["github_account_a_login"]),
        "installedPrivateRepoIdSet": bool(cfg["installed_private_repo_id"]),
        "outOfInstallRepoIdSet": bool(cfg["out_of_install_repo_id"]),
        "holderWillClickGithubUi": cfg["holder_will_click_github_ui"],
        "restoreFlags": cfg["restore_flags"],
        "databaseUrlEnvVar": cfg["database_url_env_var"],
        "databaseConnectionUrlFileSet": bool(cfg["database_connection_url_file"]),
        "httpsCookieNote": cfg["https_cookie_note"],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Load verify-repomesh operator YAML")
    parser.add_argument("--config", help="absolute or cwd-relative yaml path")
    parser.add_argument("--export", action="store_true", help="print bash export lines")
    parser.add_argument("--json", action="store_true", help="print public JSON")
    parser.add_argument("--check", action="store_true", help="print one status line")
    args = parser.parse_args()
    try:
        path, using_example = resolve_config_path(args.config)
        cfg = validate(load_raw(path), using_example=using_example, path=path)
    except ConfigError as exc:
        sys.stderr.write(f"verify-repomesh config: {exc}\n")
        return 1
    if using_example:
        sys.stderr.write(
            f"verify-repomesh config: using example {path}; "
            "copy to config.yaml before filling live fields\n"
        )
    if args.export:
        sys.stdout.write(emit_export(cfg))
        return 0
    if args.json:
        json.dump(public_json(cfg), sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 0
    sys.stdout.write(
        f"config ok scope={cfg['run_scope']} mode={cfg['mode']} "
        f"origin={cfg['origin']} addr={cfg['addr']}\n"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
