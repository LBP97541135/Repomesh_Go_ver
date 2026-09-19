#!/usr/bin/env bash
# Installs and manages a task-owned official Collector; never forwards to cloud.
set -euo pipefail
repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
exec python3 - "$repo_root" "${1:-status}" <<'PY'
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

ROOT = Path(sys.argv[1])
ACTION = sys.argv[2]
VERSION = "0.161.0"
ARCHIVE_SHA256 = "778c689efa681ff6e4722ce9f66b9b7f57c3ba009ab2e2b43dc2e0315862c731"
ARCHIVE_NAME = f"otelcol-contrib_{VERSION}_linux_amd64.tar.gz"
URL = f"https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v{VERSION}/{ARCHIVE_NAME}"
STATE = Path(os.environ.get("REPOMESH_OBSERVE_STATE", str(Path.home() / ".local/state/repomesh-observe-20260919"))).absolute()
BINARY = STATE / "bin" / f"otelcol-contrib-{VERSION}"
MARKER = STATE / "collector-process.json"
os.umask(0o077)

def fail(message):
    raise RuntimeError(message)

def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() if hasattr(hashlib, "file_digest") else hashlib.sha256(stream.read()).hexdigest()

def save(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)

def prepare():
    if platform.system() != "Linux" or platform.machine() != "x86_64":
        fail("This pinned distribution supports Linux x86_64 only.")
    if STATE == Path.home() or STATE == Path("/") or STATE.is_symlink():
        fail("Use an owned dedicated state directory, not home, root or a symlink.")
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    if STATE.stat().st_uid != os.getuid() or STATE.stat().st_mode & 0o077:
        fail(f"State directory must be owned by this user and mode 0700: {STATE}")

def install():
    downloads = STATE / "downloads"
    downloads.mkdir(exist_ok=True)
    archive = downloads / ARCHIVE_NAME
    if not archive.exists():
        temporary = downloads / (ARCHIVE_NAME + ".partial")
        with urllib.request.urlopen(URL, timeout=60) as response, temporary.open("wb") as output:
            shutil.copyfileobj(response, output)
        temporary.replace(archive)
    if digest(archive) != ARCHIVE_SHA256:
        fail(f"Archive checksum mismatch; inspect and remove this download before retrying: {archive}")
    BINARY.parent.mkdir(exist_ok=True)
    with tarfile.open(archive, "r:gz") as bundle:
        member = bundle.getmember("otelcol-contrib")
        if not member.isfile():
            fail("Expected regular Collector binary in official archive.")
        payload = bundle.extractfile(member)
        with BINARY.with_suffix(".tmp").open("wb") as output:
            shutil.copyfileobj(payload, output)
    BINARY.with_suffix(".tmp").chmod(0o700)
    BINARY.with_suffix(".tmp").replace(BINARY)
    result = subprocess.run([str(BINARY), "--version"], check=True, capture_output=True, text=True)
    metadata = {"distribution": "opentelemetry-collector-contrib", "version": VERSION,
                "source_url": URL, "archive_sha256": ARCHIVE_SHA256,
                "binary_sha256": digest(BINARY), "version_output": result.stdout.strip()}
    save(STATE / "collector-install.json", metadata)
    print(json.dumps(metadata, ensure_ascii=False))

def identity(pid):
    try:
        proc = Path("/proc") / str(pid)
        stat = (proc / "stat").read_text().rsplit(") ", 1)[1].split()
        if stat[0] == "Z":
            return None
        return {"pid": pid, "start_ticks": stat[19],
                "executable": str((proc / "exe").resolve(strict=True)),
                "cmdline": (proc / "cmdline").read_bytes().split(b"\0")[:-1],
                "uid": proc.stat().st_uid}
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
                and actual["executable"] == str(BINARY)
                and actual["cmdline"] == [str(BINARY).encode(), ("--config=" + record["config_file"]).encode()])

def ready(endpoint):
    request = urllib.request.Request(endpoint, data=b'{"resourceSpans":[]}', headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=1) as response:
            return response.status == 200
    except (OSError, urllib.error.URLError):
        return False

def start():
    previous = read_marker()
    if owned(previous):
        previous["status"] = "running" if ready(previous["endpoint"]) else "not_ready"
        print(json.dumps(previous, ensure_ascii=False))
        return
    if not BINARY.is_file():
        fail("Collector not installed; run scripts/observe-local.sh install first.")
    installed = json.loads((STATE / "collector-install.json").read_text())
    if digest(BINARY) != installed["binary_sha256"]:
        fail("Collector binary differs from its verified install record.")
    port = int(os.environ.get("REPOMESH_OTLP_PORT", "14318"))
    if not 1024 <= port <= 65535:
        fail("REPOMESH_OTLP_PORT must be between 1024 and 65535.")
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(("127.0.0.1", port))
    launch_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
    run_dir = STATE / "collector-runs" / launch_id
    run_dir.mkdir(parents=True)
    config = run_dir / "config.yaml"
    shutil.copyfile(ROOT / "configs/otel-collector.local.yaml", config)
    trace_file = run_dir / "traces.jsonl"
    log_file = run_dir / "collector.log"
    environment = dict(os.environ, REPOMESH_OTLP_PORT=str(port), REPOMESH_OTEL_TRACE_FILE=str(trace_file), GOMEMLIMIT="100MiB")
    subprocess.run([str(BINARY), "validate", "--config=" + str(config)], env=environment, check=True)
    with log_file.open("ab") as output:
        process = subprocess.Popen([str(BINARY), "--config=" + str(config)], env=environment,
                                   stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                   start_new_session=True, close_fds=True)
    actual = identity(process.pid)
    if actual is None:
        fail(f"Collector exited during startup; inspect {log_file}")
    record = {"pid": process.pid, "start_ticks": actual["start_ticks"], "version": VERSION,
              "endpoint": f"http://127.0.0.1:{port}/v1/traces", "config_file": str(config),
              "trace_file": str(trace_file), "log_file": str(log_file), "started_at": launch_id}
    save(MARKER, record)
    for _ in range(50):
        if process.poll() is not None:
            fail(f"Collector exited with {process.returncode}; inspect {log_file}")
        if ready(record["endpoint"]):
            record["status"] = "running"
            print(json.dumps(record, ensure_ascii=False))
            return
        time.sleep(0.1)
    fail(f"Collector not ready yet; inspect {log_file} or stop it using this script.")

def stop():
    record = read_marker()
    if not owned(record):
        print(json.dumps({"status": "not_running", "detail": "No matching owned process; no signal sent."}))
        return
    # A pidfd keeps PID reuse between verification and signalling from affecting
    # a different process. Verify ownership again after obtaining the handle.
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
    fail("Collector has not stopped after 10 seconds; no force-kill was performed.")

try:
    if ACTION not in {"install", "start", "stop", "status"}:
        fail("Usage: scripts/observe-local.sh {install|start|status|stop}")
    prepare()
    with (STATE / "collector.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if ACTION == "install":
            if owned(read_marker()):
                fail("Stop this Collector before reinstalling its binary.")
            install()
        elif ACTION == "start":
            start()
        elif ACTION == "stop":
            stop()
        else:
            record = read_marker() or {}
            record["status"] = ("running" if ready(record["endpoint"]) else "not_ready") if owned(record) else "not_running"
            print(json.dumps(record, ensure_ascii=False))
except (OSError, ValueError, RuntimeError, KeyError, subprocess.CalledProcessError) as error:
    print(f"observe-local: {error}", file=sys.stderr)
    sys.exit(1)
PY
