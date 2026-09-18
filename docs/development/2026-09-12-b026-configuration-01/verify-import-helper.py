#!/usr/bin/env python3

import json
import os
import pty
import select
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path


SCRIPT = Path(__file__).with_name("import-github-app.py")
ORIGIN = "https://repomesh.bohanxu.me:8443"
CALLBACK = ORIGIN + "/api/auth/github/callback"
SECRET = "test-secret-value"


def prepare(name, key_kind="rsa"):
    home = Path(tempfile.mkdtemp(prefix="repomesh-import-" + name + "-", dir="/tmp"))
    config_dir = home / ".config" / "repomesh"
    config_dir.mkdir(parents=True, mode=0o700)
    pending = config_dir / "auth.pending.json"
    pending.write_text(json.dumps({
        "origin": ORIGIN,
        "appId": "",
        "clientId": "",
        "callbackUrl": CALLBACK,
        "clientSecretFile": str(config_dir / "github-client-secret"),
        "privateKeyFile": str(config_dir / "github-app.pem"),
        "activeRootId": "root-1",
        "roots": [{"id": "root-1", "path": str(config_dir / "root-1.key")}],
        "tlsCertificateFile": "",
        "tlsKeyFile": "",
    }, indent=2) + "\n")
    pending.chmod(0o600)
    key = home / "input.pem"
    if key_kind == "rsa":
        command = ["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", str(key)]
    else:
        command = ["openssl", "genpkey", "-algorithm", "EC", "-pkeyopt", "ec_paramgen_curve:P-256", "-out", str(key)]
    subprocess.run(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
    return home, config_dir, pending, key


def run(home, replies, inject_second_write_failure=False):
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["HOME"] = str(home)
        if inject_second_write_failure:
            injected = (
                "import os,runpy;"
                "real=os.open;"
                "target=os.path.join(os.path.expanduser('~'),'.config','repomesh','github-app.pem');"
                "os.open=lambda path,flags,mode=0o777: (_ for _ in ()).throw(OSError('injected')) "
                "if os.fspath(path)==target else real(path,flags,mode);"
                f"runpy.run_path({str(SCRIPT)!r},run_name='__main__')"
            )
            os.execv(sys.executable, [sys.executable, "-c", injected])
        os.execv(sys.executable, [sys.executable, str(SCRIPT)])
    output = bytearray()
    prompts = [b"GitHub App ID: ", b"GitHub Client ID: ", b"GitHub client secret: ", b"Absolute path to the GitHub App PEM file: "]
    for prompt, reply in zip(prompts, replies):
        while prompt not in output:
            ready, _, _ = select.select([fd], [], [], 5)
            if not ready:
                raise RuntimeError("prompt timeout")
            output.extend(os.read(fd, 4096))
        os.write(fd, reply.encode() + b"\n")
    while True:
        ready, _, _ = select.select([fd], [], [], 1)
        if not ready:
            break
        try:
            output.extend(os.read(fd, 4096))
        except OSError:
            break
    _, status = os.waitpid(pid, 0)
    return os.waitstatus_to_exitcode(status), output.decode("utf-8", "replace")


results = {}

home, config_dir, pending, key = prepare("success")
original = pending.read_bytes()
exit_code, transcript = run(home, ["12345", "Iv1.client", SECRET, str(key)])
if exit_code != 0:
    raise RuntimeError(transcript)
updated = json.loads(pending.read_text())
backups = list(config_dir.glob("auth.pending.json.backup-*"))
secret_file = config_dir / "github-client-secret"
private_file = config_dir / "github-app.pem"
results["success"] = {
    "exit": exit_code,
    "appId": updated.get("appId"),
    "clientId": updated.get("clientId"),
    "otherFieldsPreserved": updated.get("origin") == ORIGIN and updated.get("callbackUrl") == CALLBACK,
    "secretMode": oct(stat.S_IMODE(secret_file.stat().st_mode)),
    "privateKeyMode": oct(stat.S_IMODE(private_file.stat().st_mode)),
    "backupCount": len(backups),
    "backupExact": len(backups) == 1 and backups[0].read_bytes() == original,
    "secretAbsentFromOutput": SECRET not in transcript,
}

home2, config_dir2, pending2, key2 = prepare("existing")
existing = config_dir2 / "github-client-secret"
existing.write_bytes(b"existing-marker")
existing.chmod(0o600)
before = pending2.read_bytes()
exit_code2, transcript2 = run(home2, [])
results["existingDestination"] = {
    "exit": exit_code2,
    "existingUnchanged": existing.read_bytes() == b"existing-marker",
    "pendingUnchanged": pending2.read_bytes() == before,
    "privateKeyAbsent": not (config_dir2 / "github-app.pem").exists(),
}

home3, config_dir3, pending3, key3 = prepare("nonrsa", "ec")
before3 = pending3.read_bytes()
exit_code3, transcript3 = run(home3, ["12345", "Iv1.client", SECRET, str(key3)])
results["nonRsa"] = {
    "exit": exit_code3,
    "pendingUnchanged": pending3.read_bytes() == before3,
    "secretDestinationsAbsent": not (config_dir3 / "github-client-secret").exists() and not (config_dir3 / "github-app.pem").exists(),
    "secretAbsentFromOutput": SECRET not in transcript3,
}

home4, config_dir4, pending4, key4 = prepare("path-mismatch")
config4 = json.loads(pending4.read_text())
config4["privateKeyFile"] = str(config_dir4 / "wrong.pem")
pending4.write_text(json.dumps(config4, indent=2) + "\n")
pending4.chmod(0o600)
before4 = pending4.read_bytes()
exit_code4, transcript4 = run(home4, [])
results["pathMismatch"] = {
    "exit": exit_code4,
    "pendingUnchanged": pending4.read_bytes() == before4,
    "secretDestinationsAbsent": not (config_dir4 / "github-client-secret").exists() and not (config_dir4 / "github-app.pem").exists(),
}

home5, config_dir5, pending5, key5 = prepare("second-write-failure")
before5 = pending5.read_bytes()
exit_code5, transcript5 = run(home5, ["12345", "Iv1.client", SECRET, str(key5)], True)
secret5 = config_dir5 / "github-client-secret"
results["secondWriteFailure"] = {
    "exit": exit_code5,
    "pendingUnchanged": pending5.read_bytes() == before5,
    "firstSecretExists": secret5.is_file(),
    "firstSecretMode": oct(stat.S_IMODE(secret5.stat().st_mode)) if secret5.exists() else None,
    "secondSecretAbsent": not (config_dir5 / "github-app.pem").exists(),
    "createdPathReported": str(secret5) in transcript5,
    "secretAbsentFromOutput": SECRET not in transcript5,
}

output = Path(__file__).with_name("verification-03.json")
output.write_text(json.dumps(results, indent=2) + "\n")
print(output)

for test_home in (home, home2, home3, home4, home5):
    shutil.rmtree(test_home)
