#!/usr/bin/env python3
"""Generate the final, read-only B03 integration protection audit."""

from __future__ import annotations

import csv
import hashlib
import json
import subprocess
from pathlib import Path


ROOT = Path("/home/xubohan/projects/Repomesh_Go_ver")
SOURCE = Path("/home/xubohan/projects/Repomesh_B03")
OUT = ROOT / "docs/development/2026-09-12-b03-integration-01"
BASELINE = SOURCE / "docs/development/2026-09-12-b03-01/source-baseline.json"
I0 = OUT / "i0-candidates.tsv"
I1 = OUT / "i1-output.tsv"
TSV = OUT / "final-protection-audit.tsv"
JSON_OUT = OUT / "final-protection-audit.json"

EXCLUDED_PREFIXES = (
    ".codex/",
    "docs/archive/",
    "docs/development/",
    "validation/",
    "third_party/",
    "dist/",
    "web/dist/",
    "web/node_modules/",
)

APPROVED_FIXES = {
    "internal/projects/service.go": "ASTRA_APPROVED_I2_PROFILE_ROWS_CLOSE",
    "internal/web/project_browser_test.go": "ASTRA_APPROVED_I2_PG_HTTP_FIXTURE",
    "web/src/CreateProjectPage.tsx": "ASTRA_APPROVED_I2_CREATE_OPERATION_LOCK",
    "web/src/ProjectPage.tsx": "ASTRA_APPROVED_I2_RESOURCE_NOT_FOUND_CLEAR",
    "web/src/ProjectSettingsPage.tsx": "ASTRA_APPROVED_I2_SETTINGS_LOCK_AND_CLEAR",
}

STATUS_DOCS = {
    "README.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
    "docs/current/DEVELOPMENT-START.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
    "docs/current/HANDOFF.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
    "docs/current/IMPLEMENTATION-PLAN.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
    "docs/current/README.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
    "docs/current/project-development.md": "ASTRA_ADJUDICATED_FINAL_REVIEW_STATUS",
}


def sha(path: Path) -> str:
    if not path.is_file():
        return "ABSENT"
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(*args: str) -> bytes:
    return subprocess.check_output(args)


def git_paths(root: Path) -> set[str]:
    raw = run(
        "git",
        "-C",
        str(root),
        "ls-files",
        "--cached",
        "--others",
        "--exclude-standard",
        "-z",
    )
    return {item.decode() for item in raw.split(b"\0") if item}


def is_excluded(path: str) -> bool:
    return any(path.startswith(prefix) for prefix in EXCLUDED_PREFIXES)


def directory_inventory(path: Path) -> dict[str, object]:
    files = sorted(item for item in path.rglob("*") if item.is_file())
    digest = hashlib.sha256()
    for item in files:
        relative = item.relative_to(path).as_posix()
        digest.update(relative.encode())
        digest.update(b"\0")
        digest.update(sha(item).encode())
        digest.update(b"\n")
    return {
        "path": str(path),
        "fileCount": len(files),
        "inventorySha256": digest.hexdigest(),
    }


baseline_data = json.loads(BASELINE.read_text())
base_hashes = {entry["path"]: entry["sha256"] for entry in baseline_data["files"]}
i1_rows = list(csv.DictReader(I1.open(), delimiter="\t"))
i1_by_path = {row["path"]: row for row in i1_rows}
allowed = set(i1_by_path)

# This is the fixed I0 product/design universe: the 427-item archived baseline
# plus the B03 source inventory, with the approved protected prefixes removed.
scope = sorted(
    path
    for path in set(base_hashes) | git_paths(SOURCE)
    if not is_excluded(path)
)

rows: list[dict[str, str]] = []
changed: list[str] = []
outside: list[str] = []
progression_counts: dict[str, int] = {}
for path in scope:
    i1 = i1_by_path.get(path)
    current = sha(ROOT / path)
    baseline = base_hashes.get(path, "ABSENT")
    source_hash = sha(SOURCE / path)
    changed_from_baseline = current != baseline
    if changed_from_baseline:
        changed.append(path)
        if path not in allowed:
            outside.append(path)

    if i1 is None:
        origin = "I0_UNCHANGED"
        i1_hash = baseline
        progression = "UNCHANGED_FROM_I0_BASELINE"
    else:
        origin = i1["result"]
        i1_hash = i1["actual_sha256"]
        if path in APPROVED_FIXES:
            progression = APPROVED_FIXES[path]
        elif path in STATUS_DOCS:
            progression = STATUS_DOCS[path]
        elif current == i1_hash and origin == "APPLIED_SOURCE_EXACT":
            progression = "SOURCE_EXACT_UNCHANGED_AFTER_I1"
        elif current == i1_hash and origin == "APPLIED_ADJUDICATED":
            progression = "ASTRA_ADJUDICATED_I1_UNCHANGED"
        else:
            progression = "UNAUTHORIZED_MISMATCH"
        progression_counts[progression] = progression_counts.get(progression, 0) + 1

    rows.append(
        {
            "path": path,
            "baseline_sha256": baseline,
            "source_sha256": source_hash,
            "i1_output_sha256": i1_hash,
            "final_sha256": current,
            "i1_origin": origin,
            "final_progression": progression,
            "changed_from_i0_baseline": "yes" if changed_from_baseline else "no",
            "allowed_candidate": "yes" if path in allowed else "no",
        }
    )

with TSV.open("w", newline="") as stream:
    writer = csv.DictWriter(stream, fieldnames=list(rows[0]), delimiter="\t")
    writer.writeheader()
    writer.writerows(rows)

old_release_names = [
    "repomesh-0.1.0-b01-wsl-20260912",
    "repomesh-0.2.0-b02-local-20260912",
    "repomesh-0.2.0-b02-local-20260912-r1",
    "repomesh-0.2.0-b026-cookie-20260912-r3",
    "repomesh-0.2.0-b026-utc-20260912-r2",
]
old_releases = [directory_inventory(ROOT / "dist" / name) for name in old_release_names]
new_release = directory_inventory(
    ROOT / "dist/repomesh-0.3.0-b03-integrated-20260912-r1"
)

ps_lines = run("ps", "-eo", "pid=,args=").decode().splitlines()
b02_processes = []
for line in ps_lines:
    stripped = line.strip()
    if (
        "/dist/repomesh-0.2.0-b026-cookie-20260912-r3/bin/repomesh-web" in stripped
        or "/dist/repomesh-0.2.0-b026-cookie-20260912-r3/bin/repomesh-coordinator" in stripped
    ):
        pid, command = stripped.split(maxsplit=1)
        b02_processes.append({"pid": int(pid), "command": command})

evidence_inventory = []
for item in sorted(path for path in OUT.rglob("*") if path.is_file()):
    if item in {TSV, JSON_OUT}:
        continue
    rel = item.relative_to(ROOT).as_posix()
    if item.name.endswith((".py", ".mjs", ".sh")):
        category = "validation_script"
    elif rel.endswith(("README.md", "checks.md")):
        category = "integration_record"
    else:
        category = "validation_evidence"
    evidence_inventory.append(
        {"path": rel, "sha256": sha(item), "category": category}
    )

b04_paths = [
    ROOT / "docs/current/NEXT-TASK-B04-PROMPT.md",
    ROOT / "docs/development/2026-09-12-b04-handoff-01/HANDOFF.md",
    ROOT / "docs/development/2026-09-12-b04-handoff-01/NEXT-SESSION-PROMPT.md",
]

index_clean = subprocess.run(
    ["git", "-C", str(ROOT), "diff", "--cached", "--quiet"], check=False
).returncode == 0

audit = {
    "status": "PASS_PROTECTION_AUDIT",
    "root": str(ROOT),
    "head": run("git", "-C", str(ROOT), "rev-parse", "HEAD").decode().strip(),
    "expectedHead": "ad9a49b2fe3b5fe743a65c838ded6cb022e79363",
    "indexHasNoStagedDiff": index_clean,
    "i0Baseline": {
        "archivedPathCount": len(base_hashes),
        "scopePathCount": len(scope),
        "unchangedPathCount": len(scope) - len(changed),
        "changedPathCount": len(changed),
        "allowedCandidateCount": len(allowed),
        "outsideAllowedCount": len(outside),
        "outsideAllowedPaths": outside,
        "sourceBaselineJsonSha256": sha(BASELINE),
        "i0CandidatesSha256": sha(I0),
        "i1OutputSha256": sha(I1),
        "i0ChecksSha256": sha(OUT / "checks.md"),
    },
    "candidates": {
        "total": len(i1_rows),
        "sourceExact": sum(row["result"] == "APPLIED_SOURCE_EXACT" for row in i1_rows),
        "adjudicated": sum(row["result"] == "APPLIED_ADJUDICATED" for row in i1_rows),
        "progressionCounts": dict(sorted(progression_counts.items())),
        "unauthorizedMismatchCount": progression_counts.get("UNAUTHORIZED_MISMATCH", 0),
        "perPathEvidence": TSV.name,
    },
    "protected": {
        "codexConfig": {
            "path": ".codex/config.toml",
            "sha256": sha(ROOT / ".codex/config.toml"),
            "expectedI0Sha256": "58c3d47686d46de2074c7acc7bb897d41807b4ed89fad8d20482448048e5cbad",
            "contentRead": False,
        },
        "secondAccountPause": {
            "path": "docs/development/2026-09-12-b026-second-account-02/PAUSE.md",
            "sha256": sha(
                ROOT
                / "docs/development/2026-09-12-b026-second-account-02/PAUSE.md"
            ),
            "expectedI0Sha256": "3da54c323316e1ae8a2f5ea131c85495cc409958315917886b8e87341b9d7cbc",
            "contentRead": False,
        },
    },
    "releases": {
        "oldDirectories": old_releases,
        "oldDirectoryCount": len(old_releases),
        "oldFileCount": sum(item["fileCount"] for item in old_releases),
        "newIndependentDirectory": new_release,
        "totalDirectoryCount": len(old_releases) + 1,
    },
    "b02R3Processes": b02_processes,
    "integrationEvidence": evidence_inventory,
    "b04DocumentsCountedSeparately": [
        {
            "path": path.relative_to(ROOT).as_posix(),
            "sha256": sha(path),
            "exists": path.is_file(),
        }
        for path in b04_paths
    ],
    "secretBodiesRead": False,
}

JSON_OUT.write_text(json.dumps(audit, ensure_ascii=False, indent=2) + "\n")

if len(scope) != 423 or len(changed) != 60 or outside:
    raise SystemExit("scope protection assertion failed")
if len(i1_rows) != 60 or audit["candidates"]["sourceExact"] != 53:
    raise SystemExit("candidate count assertion failed")
if audit["candidates"]["adjudicated"] != 7:
    raise SystemExit("adjudicated count assertion failed")
if audit["candidates"]["unauthorizedMismatchCount"] != 0:
    raise SystemExit("unauthorized candidate progression")
if audit["head"] != audit["expectedHead"] or not index_clean:
    raise SystemExit("Git protection assertion failed")
if audit["protected"]["codexConfig"]["sha256"] != audit["protected"]["codexConfig"]["expectedI0Sha256"]:
    raise SystemExit(".codex/config.toml hash changed")
if audit["protected"]["secondAccountPause"]["sha256"] != audit["protected"]["secondAccountPause"]["expectedI0Sha256"]:
    raise SystemExit("second-account PAUSE hash changed")
if audit["releases"]["oldFileCount"] != 59 or new_release["fileCount"] != 12:
    raise SystemExit("release inventory assertion failed")
if len(b02_processes) != 2 or not all("r3" in item["command"] for item in b02_processes):
    raise SystemExit("B02 r3 process assertion failed")

print(JSON_OUT)
print(TSV)
