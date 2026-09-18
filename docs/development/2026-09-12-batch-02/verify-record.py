"""Recheck local links, inherited files, and final B02 evidence without services."""

import hashlib
import json
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import unquote

batch = Path(__file__).resolve().parent
root = batch.parents[2]
preflight = batch.parent / "2026-09-12-b02-preflight"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(name, value):
    (batch / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def git(*args):
    return subprocess.run(["git", *args], cwd=root, text=True, capture_output=True, check=True).stdout


baseline = json.loads((preflight / "baseline.json").read_text())
inherited = baseline["existingChanges"] + json.loads((preflight / "baseline-supplement.json").read_text())["existingChanges"]
authorized = {
    "README.md",
    "docs/current/HANDOFF.md",
    "docs/current/IMPLEMENTATION-PLAN.md",
    "docs/current/wsl-development-recommendation.md",
    "docs/development/2026-09-12-wsl-ssh/README.md",
    "scripts/verify-batch.ps1",
}
preserved = []
for item in inherited:
    current = root / item["path"]
    backup = Path(baseline["backupDirectory"]) / item["path"]
    preserved.append({
        "path": item["path"],
        "unchanged": current.is_file() and digest(current) == item["sha256"],
        "originalBackupMatches": backup.is_file() and digest(backup) == item["sha256"],
        "authorizedTaskUpdate": item["path"] in authorized,
    })

product = [root / "go.mod", root / "go.sum", root / "web/package.json", root / "web/package-lock.json", root / "web/index.html"]
for folder in ["cmd", "internal", "configs", "scripts", "web/src"]:
    product.extend(p for p in (root / folder).rglob("*") if p.is_file())
write("final-source.json", {
    "recordedAt": datetime.now(timezone.utc).isoformat(),
    "head": git("rev-parse", "HEAD").strip(),
    "branch": git("branch", "--show-current").strip(),
    "worktree": "uncommitted; includes inherited changes",
    "sha256": {str(p.relative_to(root)): digest(p) for p in sorted(set(product))},
})


files = set(git("diff", "--name-only", "-z").split("\0"))
files.update(git("ls-files", "--others", "--exclude-standard", "-z").split("\0"))
documents = sorted(root / name for name in files if name.endswith(".md") and (root / name).is_file())
links = []
for doc in documents:
    body = re.sub(r"(?ms)^```.*?^```[^\n]*", "", doc.read_text())
    for target in re.findall(r"\]\(([^)]+)\)", body):
        target = target.strip().strip("<>")
        if target.startswith("#") or re.match(r"^[a-zA-Z]+://", target):
            continue
        target = re.sub(r":\d+$", "", unquote(target.split("#", 1)[0]))
        resolved = (doc.parent / target).resolve()
        links.append({"source": str(doc.relative_to(root)), "target": target, "exists": resolved.is_file() or resolved.is_dir()})


verification = json.loads((batch / "verification-02/checks.json").read_text())
browser = json.loads((batch / "browser-06/checks.json").read_text())
release = json.loads((batch / "release-verification-r1.json").read_text())
events = []
for line in next(c for c in verification["checks"] if c["name"] == "go-test")["stdout"].splitlines():
    if line.startswith("{"):
        events.append(json.loads(line))
top_passes = [e for e in events if e.get("Action") == "pass" and e.get("Test") and "/" not in e["Test"]]
postgres = [e for e in events if e.get("Test", "").startswith("TestPostgres")]
diff = subprocess.run(["git", "-c", "core.safecrlf=false", "diff", "--check"], cwd=root, text=True, capture_output=True)
result = {
    "recordedAt": datetime.now(timezone.utc).isoformat(),
    "scope": "Local structural checks; does not repeat product or external integration tests. Markdown target existence only, not anchor validation.",
    "documentsChecked": len(documents),
    "localLinkCount": len(links),
    "linkFailures": [link for link in links if not link["exists"] and (root / link["source"]).parent.joinpath(link["target"]).resolve() != batch / "final-checks.json"],
    "preservedFiles": preserved,
    "preservationPassed": all(p["originalBackupMatches"] and (p["unchanged"] or p["authorizedTaskUpdate"]) for p in preserved),
    "gitDiffCheck": {"exitCode": diff.returncode, "stdout": diff.stdout, "stderr": diff.stderr},
    "evidence": {
        "standardChecks": len(verification["checks"]),
        "standardResult": verification["result"],
        "cleanupPassed": verification["cleanupPassed"],
        "topLevelGoPasses": len(top_passes),
        "topLevelPostgresPasses": sum(e["Test"].startswith("TestPostgres") for e in top_passes),
        "postgresPassEvents": sum(e["Action"] == "pass" for e in postgres),
        "postgresSkips": sum(e["Action"] == "skip" for e in postgres),
        "skippedNamedTests": [e["Test"] for e in events if e.get("Action") == "skip" and e.get("Test")],
        "browserChecks": len(browser["checks"]),
        "browserResult": browser["result"],
        "releaseVersion": release["version"],
        "releaseResult": release["result"],
        "externalGitHub": browser["externalGitHub"],
    },
}
result["result"] = "PASS" if not result["linkFailures"] and result["preservationPassed"] and diff.returncode == 0 else "FAIL"
write("final-checks.json", result)
print(json.dumps({k: result[k] for k in ["result", "documentsChecked", "localLinkCount", "linkFailures", "preservationPassed", "evidence"]}, ensure_ascii=False))
raise SystemExit(0 if result["result"] == "PASS" else 1)
