#!/usr/bin/env bash
# Manage the repository's local workbench. No cloud installation or account.
set -euo pipefail
repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
exec python3 - "$repo_root" "${1:-status}" <<'PY'
import fcntl
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.request

ROOT = Path(sys.argv[1])
ACTION = sys.argv[2]
STATE = Path(os.environ.get("REPOMESH_WORKBENCH_STATE", str(Path.home() / ".local/state/repomesh-observe-local"))).absolute()
BINARY = STATE / "bin/repomesh-observe"
MARKER = STATE / "process.json"
os.umask(0o077)

def fail(message):
    raise RuntimeError(message)

def save(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)

def identity(pid):
    try:
        proc = Path("/proc") / str(pid)
        stat = (proc / "stat").read_text().rsplit(") ", 1)[1].split()
        if stat[0] == "Z":
            return None
        return {"start_ticks": stat[19], "executable": str((proc / "exe").resolve(strict=True)),
                "command": (proc / "cmdline").read_bytes().split(b"\0")[:-1], "uid": proc.stat().st_uid}
    except (OSError, ValueError):
        return None

def record():
    return json.loads(MARKER.read_text()) if MARKER.exists() else None

def owned(value):
    if not value:
        return False
    actual = identity(value["pid"])
    return bool(actual and actual["uid"] == os.getuid() and actual["start_ticks"] == value["start_ticks"]
                and actual["executable"] == str(BINARY)
                and actual["command"] == [s.encode() for s in value["command"]])

def ready(url):
    try:
        with urllib.request.urlopen(url + "/api/health", timeout=1) as response:
            return json.load(response).get("service") == "repomesh-local-observe"
    except (OSError, ValueError):
        return False

def install():
    if owned(record()):
        fail("Stop this workbench before rebuilding its binary.")
    BINARY.parent.mkdir(exist_ok=True)
    temporary = BINARY.with_suffix(".new")
    subprocess.run(["go", "build", "-o", str(temporary), "./cmd/repomesh-observe"], cwd=ROOT, check=True)
    temporary.chmod(0o700)
    temporary.replace(BINARY)
    print(json.dumps({"status": "installed", "binary": str(BINARY)}))

def start():
    old = record()
    if owned(old):
        old["status"] = "running" if ready(old["url"]) else "not_ready"
        print(json.dumps(old))
        return
    if not BINARY.is_file():
        fail("Run scripts/observe-workbench.sh install first.")
    port = int(os.environ.get("REPOMESH_WORKBENCH_PORT", "18090"))
    if not 1024 <= port <= 65535:
        fail("REPOMESH_WORKBENCH_PORT must be between 1024 and 65535.")
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(("127.0.0.1", port))
    args = [str(BINARY), "serve", "--archive", str(STATE / "archive"), "--addr", f"127.0.0.1:{port}"]
    sources = STATE / "read-archives.json"
    if sources.exists():
        archives = json.loads(sources.read_text())
        if not isinstance(archives, list) or any(not isinstance(p, str) or not Path(p).is_absolute() for p in archives):
            fail("read-archives.json must be an array of absolute archive paths.")
        for path in archives:
            args.extend(["--read-archive", path])
    log = STATE / "workbench.log"
    with log.open("ab") as output:
        process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                   start_new_session=True, close_fds=True)
    actual = identity(process.pid)
    if actual is None:
        fail("Workbench exited during startup; inspect its private log.")
    value = {"pid": process.pid, "start_ticks": actual["start_ticks"], "command": args,
             "url": f"http://127.0.0.1:{port}", "log_file": str(log), "archive": str(STATE / "archive")}
    save(MARKER, value)
    for _ in range(50):
        if process.poll() is not None:
            fail("Workbench could not start; inspect its private log.")
        if ready(value["url"]):
            value["status"] = "running"
            print(json.dumps(value))
            return
        time.sleep(0.1)
    fail("Workbench is not ready; inspect its private log or stop it with this script.")

def stop():
    value = record()
    if not owned(value):
        print(json.dumps({"status": "not_running", "signal_sent": False}))
        return
    try:
        descriptor = os.pidfd_open(value["pid"])
        try:
            if owned(value):
                signal.pidfd_send_signal(descriptor, signal.SIGTERM)
        finally:
            os.close(descriptor)
    except ProcessLookupError:
        pass
    for _ in range(100):
        if not owned(value):
            value["status"] = "stopped"
            save(MARKER, value)
            print(json.dumps(value))
            return
        time.sleep(0.1)
    fail("Workbench did not stop in 10 seconds; no force kill performed.")

try:
    if ACTION not in {"install", "start", "stop", "status"}:
        fail("Usage: scripts/observe-workbench.sh {install|start|stop|status}")
    if STATE in {Path.home(), Path("/")} or STATE.is_symlink():
        fail("Use a dedicated state directory, not home, root or a symlink.")
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    if STATE.stat().st_uid != os.getuid() or STATE.stat().st_mode & 0o077:
        fail("State directory must be owned by this user with mode 0700.")
    with (STATE / "workbench.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if ACTION == "install":
            install()
        elif ACTION == "start":
            start()
        elif ACTION == "stop":
            stop()
        else:
            value = record() or {}
            value["status"] = ("running" if ready(value["url"]) else "not_ready") if owned(value) else "not_running"
            print(json.dumps(value))
except (OSError, ValueError, RuntimeError, KeyError, subprocess.CalledProcessError) as error:
    print(f"observe-workbench: {error}", file=sys.stderr)
    sys.exit(1)
PY
