import argparse
import hashlib
import json
import os
import platform
import stat
import subprocess
from datetime import datetime, timezone
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--repo", required=True)
parser.add_argument("--output", required=True)
args = parser.parse_args()
repo = Path(args.repo)
output = Path(args.output)
if not repo.is_absolute() or not output.is_absolute():
    parser.error("--repo and --output must be absolute paths")
repo = repo.resolve()
if not (repo / ".git").exists():
    parser.error("--repo must be a Git worktree root")
if output.exists():
    parser.error("--output must not already exist")
output.mkdir(parents=True)

prep = repo / "docs/development/2026-09-12-b02-external-preparation"
batch = repo / "docs/development/2026-09-12-batch-02"
release_dir = repo / "dist/repomesh-0.2.0-b02-local-20260912-r1"
evidence_audit = json.loads((prep / "evidence-audit.json").read_text())
final_checks = json.loads((prep / "final-checks.json").read_text())
old_evidence = json.loads((prep / "batch-02-baseline.json").read_text())
workspace_baseline = json.loads((prep / "workspace-baseline.json").read_text())
source_record = json.loads((batch / "final-source.json").read_text())
release_record = json.loads((release_dir / "release.json").read_text())

sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
git = lambda *a: subprocess.run(["git", *a], cwd=repo, text=True, capture_output=True, check=True).stdout
auth_env = os.environ.get("REPOMESH_AUTH_CONFIG", "").strip()
secret_candidates = [Path("/etc/repomesh/auth.json"), Path("/home/xubohan/.config/repomesh/auth.json")]
if auth_env:
    secret_candidates.insert(0, Path(auth_env))
configuration = {
    "environmentVariables": {
        "REPOMESH_AUTH_CONFIG": {"nonEmpty": bool(auth_env)},
        "REPOMESH_DATABASE_URL": {"nonEmpty": bool(os.environ.get("REPOMESH_DATABASE_URL", "").strip())},
    },
    "authConfigCandidates": [],
    "relatedProcessComm": [],
    "processSnapshotScope": "Current /proc namespace comm names starting with repomesh- only. Linux comm may be truncated; this does not prove that processes in other namespaces or renamed processes are absent.",
    "contentRead": False,
}
for candidate in secret_candidates:
    item = {"path": str(candidate), "exists": os.path.lexists(candidate)}
    if item["exists"]:
        info = os.lstat(candidate)
        item.update({
            "fileType": "regular" if stat.S_ISREG(info.st_mode) else "symlink" if stat.S_ISLNK(info.st_mode) else "directory" if stat.S_ISDIR(info.st_mode) else "other",
            "mode": oct(stat.S_IMODE(info.st_mode)),
            "uid": info.st_uid,
        })
    configuration["authConfigCandidates"].append(item)
for proc in Path("/proc").iterdir():
    if proc.name.isdigit():
        try:
            comm = (proc / "comm").read_text().strip()
        except OSError:
            continue
        if comm.startswith("repomesh-"):
            configuration["relatedProcessComm"].append({"pid": int(proc.name), "comm": comm})

product = [repo / "go.mod", repo / "go.sum", repo / "web/package.json", repo / "web/package-lock.json", repo / "web/index.html"]
for folder in ["cmd", "internal", "configs", "scripts", "web/src"]:
    product.extend(p for p in (repo / folder).rglob("*") if p.is_file())
secret_resolved = {str(p.resolve(strict=False)) for p in secret_candidates}
current_source = {
    str(p.relative_to(repo)): sha(p)
    for p in sorted(set(product))
    if str(p.resolve(strict=False)) not in secret_resolved
}
recorded_source = source_record["sha256"]
source_missing = sorted(set(recorded_source) - set(current_source))
source_added = sorted(set(current_source) - set(recorded_source))
source_changed = sorted(p for p in set(recorded_source) & set(current_source) if recorded_source[p] != current_source[p])

manifest = release_record["artifacts"]
package_files = sorted(str(p.relative_to(release_dir)) for p in release_dir.rglob("*") if p.is_file() and p.name != "release.json")
release_missing = sorted(set(manifest) - set(package_files))
release_added = sorted(set(package_files) - set(manifest))
release_changed = sorted(p for p in set(manifest) & set(package_files) if manifest[p] != sha(release_dir / p))

old_root = batch
recorded_old = old_evidence["sha256"]
current_old = {
    name: sha(old_root / name)
    for name in recorded_old
    if (old_root / name).is_file()
}
current_old_paths = {str(p.relative_to(old_root)) for p in old_root.rglob("*") if p.is_file()}
old_missing = sorted(set(recorded_old) - set(current_old))
old_added = sorted(current_old_paths - set(recorded_old))
old_changed = sorted(p for p in set(recorded_old) & set(current_old) if recorded_old[p] != current_old[p])

tracked = set(git("ls-files", "-z").split("\0"))
untracked = set(git("ls-files", "--others", "--exclude-standard", "-z").split("\0"))
paths = sorted((tracked | untracked) - {""})
workspace_hashes = {}
workspace_nonregular = []
workspace_missing = []
workspace_excluded_secrets = []
for name in paths:
    path = repo / name
    if str(path.resolve(strict=False)) in secret_resolved:
        workspace_excluded_secrets.append(name)
    elif not os.path.lexists(path):
        workspace_missing.append(name)
    elif stat.S_ISREG(os.lstat(path).st_mode):
        workspace_hashes[name] = sha(path)
    elif stat.S_ISLNK(os.lstat(path).st_mode):
        workspace_hashes[name] = hashlib.sha256(os.readlink(path).encode()).hexdigest()
        workspace_nonregular.append({"path": name, "fileType": "symlink", "hashInput": "link target text"})
    else:
        workspace_nonregular.append({"path": name, "fileType": "other", "hashInput": None})

baseline_hashes = workspace_baseline["sha256"]
baseline_missing = sorted(set(baseline_hashes) - set(workspace_hashes))
baseline_added = sorted(set(workspace_hashes) - set(baseline_hashes))
baseline_changed = sorted(p for p in set(baseline_hashes) & set(workspace_hashes) if baseline_hashes[p] != workspace_hashes[p])
prior_authorized = final_checks["changedExistingFiles"]
prior_comparison = []
for name in prior_authorized:
    prior_comparison.append({
        "path": name,
        "present": name in workspace_hashes,
        "differsFromEarlierWorkspaceBaseline": name in baseline_changed,
        "currentSha256": workspace_hashes.get(name),
        "earlierBaselineSha256": baseline_hashes.get(name),
        "postPreparationChangeStatus": "unknown because final-checks.json contains no final document hashes",
    })

record = {
    "recordedAt": datetime.now(timezone.utc).isoformat(),
    "environment": {
        "platform": platform.platform(),
        "python": platform.python_version(),
        "repo": str(repo),
        "output": str(output),
        "gitHead": git("rev-parse", "HEAD").strip(),
        "gitBranch": git("branch", "--show-current").strip(),
        "gitStatusPorcelainV1": git("status", "--porcelain=v1").splitlines(),
    },
    "configurationPresence": configuration,
    "sourceInventory": {
        "derivation": "Same paths as batch-02/verify-record.py, which was read but not executed",
        "recordedCount": len(recorded_source), "currentCount": len(current_source),
        "missing": source_missing, "added": source_added, "changed": source_changed,
    },
    "releaseInventory": {
        "recordedArtifactCount": len(manifest), "currentPackageFileCountExcludingManifest": len(package_files),
        "missing": release_missing, "added": release_added, "changed": release_changed,
    },
    "historicalEvidence": {
        "existingRecordPreservedWithoutRecalculation": evidence_audit,
        "recordedCount": len(recorded_old), "currentCount": len(current_old),
        "missing": old_missing, "added": old_added, "changed": old_changed,
    },
    "previousPreparationFiles": {
        "finalChecksRecordedAt": final_checks["recordedAt"],
        "finalChecksHasFinalDocumentHashes": False,
        "earlierWorkspaceBaselineRecordedAt": workspace_baseline["recordedAt"],
        "priorAuthorizedDocumentComparison": prior_comparison,
        "limitation": "The earlier workspace baseline predates the four authorized document changes. Their current hashes can be compared with that earlier baseline only. No final preparation hashes exist, so this audit cannot determine whether they changed again afterward.",
    },
    "workspacePreservation": {
        "trackedPathCount": len(tracked - {""}), "untrackedNonignoredPathCount": len(untracked - {""}),
        "hashedPathCount": len(workspace_hashes), "sha256": workspace_hashes,
        "missingTrackedPaths": workspace_missing, "nonRegularPaths": workspace_nonregular,
        "excludedSecretPaths": workspace_excluded_secrets,
        "comparisonToEarlierWorkspaceBaseline": {
            "recordedCount": len(baseline_hashes), "missing": baseline_missing,
            "added": baseline_added, "changed": baseline_changed,
            "interpretation": "This baseline predates prior authorized preparation changes and is not a final-state baseline.",
        },
        "codexConfigTreatment": "hash only" if ".codex/config.toml" in workspace_hashes else "not present",
    },
}
(output / "audit.json").write_text(json.dumps(record, ensure_ascii=False, indent=2) + "\n")
print(json.dumps({
    "output": str(output / "audit.json"),
    "configurationPresence": configuration,
    "sourceInventory": record["sourceInventory"],
    "releaseInventory": record["releaseInventory"],
    "historicalEvidence": {k: record["historicalEvidence"][k] for k in ["recordedCount", "currentCount", "missing", "added", "changed"]},
    "workspacePathCounts": {k: record["workspacePreservation"][k] for k in ["trackedPathCount", "untrackedNonignoredPathCount", "hashedPathCount"]},
}, ensure_ascii=False, indent=2))
