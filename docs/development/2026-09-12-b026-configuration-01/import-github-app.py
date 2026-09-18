#!/usr/bin/env python3

import getpass
import json
import os
import re
import stat
import subprocess
import sys
import uuid
from pathlib import Path


ORIGIN = "https://repomesh.bohanxu.me:8443"
CALLBACK = ORIGIN + "/api/auth/github/callback"
MAX_SECRET = 64 * 1024


class SafeError(Exception):
    pass


created = []


def fail(reason):
    raise SafeError(reason)


def duplicate_safe_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            fail("The pending configuration contains a duplicate JSON field.")
        result[key] = value
    return result


def write_exclusive(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    created.append(str(path))
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
    except Exception:
        raise SafeError("A newly created file could not be written completely.")


def unchanged(path, original_stat, original_bytes):
    try:
        current = os.lstat(path)
        current_bytes = path.read_bytes()
    except OSError:
        return False
    fields = ("st_dev", "st_ino", "st_uid", "st_mode", "st_size", "st_mtime_ns")
    return all(getattr(current, field) == getattr(original_stat, field) for field in fields) and current_bytes == original_bytes


def main():
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        fail("Run this script in an interactive terminal.")

    recovery_guide = Path(__file__).resolve().with_name("recovery.md")
    print("A failed import may leave newly created files in place.")
    print("Follow the metadata-only recovery steps in " + str(recovery_guide) + ".")

    config_dir = Path.home() / ".config" / "repomesh"
    pending = config_dir / "auth.pending.json"
    client_secret_path = config_dir / "github-client-secret"
    private_key_path = config_dir / "github-app.pem"

    try:
        directory_stat = os.lstat(config_dir)
    except OSError:
        fail("The RepoMesh configuration directory is unavailable.")
    if not stat.S_ISDIR(directory_stat.st_mode) or stat.S_ISLNK(directory_stat.st_mode):
        fail("The RepoMesh configuration path must be a real directory.")
    if directory_stat.st_uid != os.getuid() or stat.S_IMODE(directory_stat.st_mode) != 0o700:
        fail("The RepoMesh configuration directory must be owned by the current user with mode 0700.")

    try:
        pending_stat = os.lstat(pending)
    except OSError:
        fail("The pending configuration file is unavailable.")
    if not stat.S_ISREG(pending_stat.st_mode) or stat.S_ISLNK(pending_stat.st_mode):
        fail("The pending configuration must be a regular file and not a symbolic link.")
    if pending_stat.st_uid != os.getuid() or stat.S_IMODE(pending_stat.st_mode) != 0o600:
        fail("The pending configuration must be owned by the current user with mode 0600.")
    try:
        pending_bytes = pending.read_bytes()
        config = json.loads(pending_bytes, object_pairs_hook=duplicate_safe_object)
    except SafeError:
        raise
    except Exception:
        fail("The pending configuration is not valid JSON.")
    if not isinstance(config, dict):
        fail("The pending configuration must contain a JSON object.")
    if config.get("origin") != ORIGIN or config.get("callbackUrl") != CALLBACK:
        fail("The pending configuration does not contain the fixed origin and callback URL.")
    if config.get("clientSecretFile") != str(client_secret_path) or config.get("privateKeyFile") != str(private_key_path):
        fail("The pending configuration does not contain the fixed secret file paths.")

    if client_secret_path.exists() or client_secret_path.is_symlink() or private_key_path.exists() or private_key_path.is_symlink():
        fail("A destination secret file already exists. Nothing was written.")

    app_id = input("GitHub App ID: ").strip()
    if not re.fullmatch(r"[0-9]+", app_id) or int(app_id) <= 0:
        fail("The App ID must be an ASCII positive integer.")
    client_id = input("GitHub Client ID: ")
    if not client_id or any(character.isspace() for character in client_id):
        fail("The Client ID must be nonempty and contain no whitespace.")
    client_secret = getpass.getpass("GitHub client secret: ").encode("utf-8")
    if not client_secret or len(client_secret) > MAX_SECRET:
        fail("The client secret must be nonempty and no larger than 64 KiB.")
    if b"\x00" in client_secret or b"\r" in client_secret or b"\n" in client_secret:
        fail("The client secret must not contain NUL or newline characters.")

    key_input = input("Absolute path to the GitHub App PEM file: ").strip()
    if len(key_input) >= 2 and key_input[0] == key_input[-1] == '"':
        key_input = key_input[1:-1]
    if re.match(r"^[A-Za-z]:[\\/]", key_input):
        try:
            converted = subprocess.run(
                ["wslpath", "-a", key_input], capture_output=True, text=True, check=True
            ).stdout.strip()
        except Exception:
            fail("The Windows PEM path could not be converted with wslpath.")
        key_input = converted
    key_source = Path(key_input)
    if not key_source.is_absolute():
        fail("The PEM path must be absolute.")
    try:
        key_stat = os.lstat(key_source)
    except OSError:
        fail("The PEM file is unavailable.")
    if not stat.S_ISREG(key_stat.st_mode) or stat.S_ISLNK(key_stat.st_mode):
        fail("The PEM source must be a regular file and not a symbolic link.")
    if key_stat.st_size <= 0 or key_stat.st_size > MAX_SECRET:
        fail("The PEM file must be nonempty and no larger than 64 KiB.")
    try:
        key_fd = os.open(key_source, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
        with os.fdopen(key_fd, "rb") as key_stream:
            opened_stat = os.fstat(key_stream.fileno())
            if opened_stat.st_dev != key_stat.st_dev or opened_stat.st_ino != key_stat.st_ino:
                fail("The PEM source changed before it could be read.")
            key_bytes = key_stream.read(MAX_SECRET + 1)
    except SafeError:
        raise
    except OSError:
        fail("The PEM file could not be read safely.")
    if len(key_bytes) != key_stat.st_size or len(key_bytes) > MAX_SECRET:
        fail("The PEM source changed while it was being read.")
    try:
        checked = subprocess.run(
            ["openssl", "rsa", "-passin", "pass:", "-check", "-modulus", "-noout"],
            input=key_bytes,
            capture_output=True,
            check=False,
        )
    except OSError:
        fail("OpenSSL is unavailable.")
    if checked.returncode != 0:
        fail("The PEM must contain a valid, unencrypted RSA private key.")
    match = re.search(rb"(?m)^Modulus=([0-9A-Fa-f]+)\r?$", checked.stdout)
    if match is None or int(match.group(1), 16).bit_length() < 2048:
        fail("The RSA private key must be at least 2048 bits.")

    config["appId"] = app_id
    config["clientId"] = client_id
    updated = (json.dumps(config, ensure_ascii=False, indent=2) + "\n").encode("utf-8")

    current_dir = os.lstat(config_dir)
    if (
        current_dir.st_dev != directory_stat.st_dev
        or current_dir.st_ino != directory_stat.st_ino
        or current_dir.st_uid != directory_stat.st_uid
        or current_dir.st_mode != directory_stat.st_mode
    ):
        fail("The RepoMesh configuration directory changed during input.")
    if not unchanged(pending, pending_stat, pending_bytes):
        fail("The pending configuration changed during input.")

    write_exclusive(client_secret_path, client_secret)
    write_exclusive(private_key_path, key_bytes)
    backup = config_dir / ("auth.pending.json.backup-" + uuid.uuid4().hex)
    write_exclusive(backup, pending_bytes)
    temporary = config_dir / ("auth.pending.json.tmp-" + uuid.uuid4().hex)
    write_exclusive(temporary, updated)
    if not unchanged(pending, pending_stat, pending_bytes):
        fail("The pending configuration changed before replacement.")
    os.replace(temporary, pending)
    created.remove(str(temporary))
    print("Import completed. Created paths:")
    for path in created:
        print(path)


if __name__ == "__main__":
    try:
        main()
    except SafeError as error:
        print("Import stopped: " + str(error), file=sys.stderr)
        if created:
            print("Created paths:", file=sys.stderr)
            for item in created:
                print(item, file=sys.stderr)
        raise SystemExit(1)
    except Exception:
        print("Import stopped because of an unexpected local error.", file=sys.stderr)
        if created:
            print("Created paths:", file=sys.stderr)
            for item in created:
                print(item, file=sys.stderr)
        raise SystemExit(1)
