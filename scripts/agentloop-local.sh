#!/usr/bin/env bash
# Isolated official AgentLoop Launcher. No cloud profile or experiment is created.
set -euo pipefail
exec python3 - "${1:-status}" <<'PY'
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import socket
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.request

ACTION = sys.argv[1]
SOURCE_URL = "https://github.com/aliyun/agentloop_experiment_manager.git"
SOURCE_COMMIT = "be1dbf6629016fdff74a696ed22afeb207e74422"
UV_VERSION = "0.12.17"
UV_SHA256 = "fa82fd8dde8e8eefdecada6aa0889666556cfceb690d06e0c3bca49eb3070a63"
UV_URL = f"https://github.com/astral-sh/uv/releases/download/{UV_VERSION}/uv-x86_64-unknown-linux-gnu.tar.gz"
PYTHON_VERSION = "3.12.11"
STATE = Path(os.environ.get("REPOMESH_OBSERVE_STATE", str(Path.home() / ".local/state/repomesh-observe-20260919"))).absolute()
SOURCE = STATE / "agentloop-source"
UV = STATE / "bin" / ("uv-" + UV_VERSION)
PYTHON = SOURCE / "backend/.venv/bin/python"
DATA = STATE / "agentloop-data"
MARKER = STATE / "agentloop-process.json"
INSTALL = STATE / "agentloop-install.json"
os.umask(0o077)

def fail(message):
    raise RuntimeError(message)

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def save(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)

def run(args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)

def environment():
    # Do not inherit another Launcher's data directory, gateway or master key.
    result = {key: value for key, value in os.environ.items() if not key.startswith("AGENTLOOP_LAUNCHER_")}
    result.update(UV_CACHE_DIR=str(STATE / "cache/uv"), UV_PYTHON_INSTALL_DIR=str(STATE / "python"),
                  UV_NO_PROGRESS="1", UV_PYTHON_BIN_DIR=str(STATE / "bin"),
                  AGENTLOOP_LAUNCHER_GATEWAY_MODE="agentloop")
    return result

def prepare():
    if platform.system() != "Linux" or platform.machine() != "x86_64":
        fail("This setup supports Linux x86_64 only.")
    if STATE in {Path.home(), Path("/")} or STATE.is_symlink():
        fail("Use an owned dedicated state directory, not home, root or a symlink.")
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    if STATE.stat().st_uid != os.getuid() or STATE.stat().st_mode & 0o077:
        fail(f"State directory must be owned by this user and mode 0700: {STATE}")

def source_identity():
    revision = run(["git", "-C", SOURCE, "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
    if revision != SOURCE_COMMIT:
        fail(f"Source is not the pinned commit {SOURCE_COMMIT}; existing source left unchanged.")
    run(["git", "-C", SOURCE, "diff", "--exit-code", "HEAD", "--"], stdout=subprocess.DEVNULL)
    return revision

def install():
    downloads = STATE / "downloads"
    downloads.mkdir(exist_ok=True)
    archive = downloads / f"uv-{UV_VERSION}-linux-amd64.tar.gz"
    if not archive.exists():
        temporary = archive.with_suffix(".partial")
        with urllib.request.urlopen(UV_URL, timeout=60) as response, temporary.open("wb") as output:
            shutil.copyfileobj(response, output)
        temporary.replace(archive)
    if digest(archive) != UV_SHA256:
        fail(f"uv archive checksum mismatch: {archive}")
    UV.parent.mkdir(exist_ok=True)
    with tarfile.open(archive, "r:gz") as bundle:
        member = bundle.getmember("uv-x86_64-unknown-linux-gnu/uv")
        if not member.isfile():
            fail("Expected regular uv executable in official archive.")
        with UV.with_suffix(".tmp").open("wb") as output:
            shutil.copyfileobj(bundle.extractfile(member), output)
    UV.with_suffix(".tmp").chmod(0o700)
    UV.with_suffix(".tmp").replace(UV)
    if not SOURCE.exists():
        run(["git", "init", SOURCE])
        run(["git", "-C", SOURCE, "remote", "add", "origin", SOURCE_URL])
        run(["git", "-C", SOURCE, "fetch", "--depth", "1", "origin", SOURCE_COMMIT])
        run(["git", "-C", SOURCE, "checkout", "--detach", "FETCH_HEAD"])
    source_identity()
    env = environment()
    run([UV, "sync", "--locked", "--no-dev", "--project", SOURCE / "backend", "--python", PYTHON_VERSION], env=env)
    run(["npm", "ci", "--prefix", SOURCE / "frontend", "--cache", STATE / "cache/npm", "--no-audit", "--no-fund"], env=env)
    run(["npm", "run", "build", "--prefix", SOURCE / "frontend"], env=env)
    source_identity()
    metadata = {"name": "AgentLoop Local Experiment Launcher", "source_url": SOURCE_URL,
                "source_commit": SOURCE_COMMIT, "uv_version": UV_VERSION,
                "uv_archive_sha256": UV_SHA256, "python_version": PYTHON_VERSION,
                "python_runtime": str(PYTHON.resolve()),
                "backend_lock_sha256": digest(SOURCE / "backend/uv.lock"),
                "frontend_lock_sha256": digest(SOURCE / "frontend/package-lock.json"),
                "frontend_index_sha256": digest(SOURCE / "backend/app/static/index.html"),
                "cloud_connection": "not_configured", "gateway_mode": "agentloop"}
    save(INSTALL, metadata)
    print(json.dumps(metadata, ensure_ascii=False))

def identity(pid):
    try:
        proc = Path("/proc") / str(pid)
        stat = (proc / "stat").read_text().rsplit(") ", 1)[1].split()
        if stat[0] == "Z":
            return None
        return {"pid": pid, "start_ticks": stat[19], "executable": str((proc / "exe").resolve(strict=True)),
                "cmdline": (proc / "cmdline").read_bytes().split(b"\0")[:-1], "uid": proc.stat().st_uid}
    except (FileNotFoundError, PermissionError, ProcessLookupError):
        return None

def read_marker():
    return json.loads(MARKER.read_text()) if MARKER.exists() else None

def owned(record):
    if not record:
        return False
    actual = identity(record["pid"])
    return bool(actual and actual["uid"] == os.getuid()
                and actual["start_ticks"] == record["start_ticks"]
                and actual["executable"] == str(PYTHON.resolve())
                and actual["cmdline"] == [part.encode() for part in record["command"]])

def health(url):
    try:
        with urllib.request.urlopen(url + "/api/v1/health", timeout=1) as response:
            value = json.load(response)
            return value if response.status == 200 and value.get("gatewayMode") == "agentloop" else None
    except (OSError, ValueError, urllib.error.URLError):
        return None

def command(port, action="start"):
    return [str(PYTHON), "-m", "app.cli", action, "--host", "127.0.0.1", "--port", str(port),
            "--environment", "development", "--data-dir", str(DATA), "--no-open-browser", "--no-scheduler"]

def verify_install():
    if not INSTALL.exists() or not PYTHON.is_file():
        fail("Launcher not installed; run scripts/agentloop-local.sh install first.")
    source_identity()
    installed = json.loads(INSTALL.read_text())
    for key, path in [("backend_lock_sha256", "backend/uv.lock"),
                      ("frontend_lock_sha256", "frontend/package-lock.json"),
                      ("frontend_index_sha256", "backend/app/static/index.html")]:
        if digest(SOURCE / path) != installed[key]:
            fail(f"Installed source/build changed: {path}")

def start():
    previous = read_marker()
    if owned(previous):
        previous["health"] = health(previous["url"])
        previous["status"] = "running" if previous["health"] else "not_ready"
        print(json.dumps(previous, ensure_ascii=False))
        return
    verify_install()
    port = int(os.environ.get("REPOMESH_AGENTLOOP_PORT", "18090"))
    if not 1024 <= port <= 65535:
        fail("REPOMESH_AGENTLOOP_PORT must be between 1024 and 65535.")
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(("127.0.0.1", port))
    launch_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
    log = STATE / "agentloop-runs" / launch_id / "launcher.log"
    log.parent.mkdir(parents=True)
    args = command(port)
    with log.open("ab") as output:
        process = subprocess.Popen(args, cwd=SOURCE / "backend", env=environment(),
                                   stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                   start_new_session=True, close_fds=True)
    actual = identity(process.pid)
    if actual is None:
        fail(f"Launcher exited during startup; inspect {log}")
    record = {"pid": process.pid, "start_ticks": actual["start_ticks"], "command": args,
              "url": f"http://127.0.0.1:{port}", "source_commit": SOURCE_COMMIT,
              "data_dir": str(DATA), "log_file": str(log), "started_at": launch_id}
    save(MARKER, record)
    for _ in range(100):
        if process.poll() is not None:
            fail(f"Launcher exited with {process.returncode}; inspect {log}")
        result = health(record["url"])
        if result:
            record.update(status="running", health=result)
            print(json.dumps(record, ensure_ascii=False))
            return
        time.sleep(0.1)
    fail(f"Launcher not ready yet; inspect {log} or stop it using this script.")

def stop():
    record = read_marker()
    if not owned(record):
        print(json.dumps({"status": "not_running", "detail": "No matching owned process; no signal sent."}))
        return
    try:
        descriptor = os.pidfd_open(record["pid"])
        try:
            if owned(record):
                signal.pidfd_send_signal(descriptor, signal.SIGTERM)
        finally:
            os.close(descriptor)
    except ProcessLookupError:
        pass
    for _ in range(100):
        if not owned(record):
            record["status"] = "stopped"
            save(MARKER, record)
            print(json.dumps(record, ensure_ascii=False))
            return
        time.sleep(0.1)
    fail("Launcher has not stopped after 10 seconds; no force-kill was performed.")

try:
    if ACTION not in {"install", "start", "stop", "status", "doctor"}:
        fail("Usage: scripts/agentloop-local.sh {install|start|status|stop|doctor}")
    prepare()
    with (STATE / "agentloop.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if ACTION == "install":
            if owned(read_marker()):
                fail("Stop this Launcher before reinstalling.")
            install()
        elif ACTION == "start":
            start()
        elif ACTION == "stop":
            stop()
        elif ACTION == "doctor":
            verify_install()
            run(command(int(os.environ.get("REPOMESH_AGENTLOOP_PORT", "18090")), "doctor"),
                cwd=SOURCE / "backend", env=environment())
        else:
            record = read_marker() or {}
            running = owned(record)
            record["health"] = health(record["url"]) if running else None
            record["status"] = ("running" if record["health"] else "not_ready") if running else "not_running"
            print(json.dumps(record, ensure_ascii=False))
except (OSError, ValueError, RuntimeError, KeyError, subprocess.CalledProcessError) as error:
    print(f"agentloop-local: {error}", file=sys.stderr)
    sys.exit(1)
PY
