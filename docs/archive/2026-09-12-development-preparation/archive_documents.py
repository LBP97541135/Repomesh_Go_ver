"""Archive superseded navigation/history; preserve original bytes and relocate links."""
import argparse
import hashlib
import json
import os
import re
import shutil
import zipfile
from pathlib import Path
from urllib.parse import unquote

DEST = Path(__file__).resolve().parent
ROOT = DEST.parents[2]
HISTORY = [
    "design-communication.md", "design-communication-page-log.md",
    "design-communication-backend-log.md", "design-communication-page-2026-09-10.md",
    "design-communication-backend-2026-09-10.md", "design-communication-page-2026-09-11.md",
    "design-communication-backend-2026-09-11.md", "NEXT-SESSION-PROMPT.md",
    "NEXT-BACKEND-SESSION-PROMPT.md", "page-interface-prototype.md",
    "draft-issue-and-room-entry-design.md", "project-first-entry-design.md",
    "changeset-design-discussion.md", "document-review-2026-09-09.md",
    "scaffold-source-inventory.md", "scaffold-adr-review.md", "scaffold-page-backend-review.md",
    "HANDOFF-BACKEND-DESIGN-2026-09-11.md", "first-development-todo.md",
]
OLD_HTML = ["repomesh-conversation-navigation-prototype.html",
            "repomesh-model-settings-prototype.html", "repomesh-project-first.html",
            "repomesh-rooms-prototype.html"]
SNAPSHOTS = ["docs/current/HANDOFF.md", "docs/current/README.md",
             "docs/current/HANDOFF-PAGE-API-DESIGN.md", "docs/current/HANDOFF-BACKEND-DESIGN.md",
             "docs/prototypes/README.md", "docs/README.md"]


def target_for(source):
    return DEST / source.relative_to(ROOT)


def relocate(raw, old_path, new_path, mapping):
    source = raw.decode("utf-8-sig")
    def replace(match):
        target = match.group(2)
        clean = target.strip().strip("<>")
        if re.match(r"(?:https?://|mailto:|codex:|#)", clean):
            return match.group(0)
        file_part, sep, anchor = clean.partition("#")
        if not file_part:
            return match.group(0)
        resolved = (old_path.parent / unquote(file_part)).resolve()
        if not resolved.is_relative_to(ROOT):
            return match.group(0)
        destination = mapping.get(resolved, resolved)
        if destination == resolved and old_path.parent == new_path.parent:
            return match.group(0)
        relative = os.path.relpath(destination, new_path.parent).replace("\\", "/")
        if sep:
            relative += "#" + anchor
        return match.group(1) + relative + match.group(3)
    # Keep examples and quoted command bodies byte-for-byte inside fenced blocks.
    parts = re.split(r"(^```[^\n]*\n.*?^```[^\n]*(?:\n|$))", source, flags=re.M | re.S)
    for number in range(0, len(parts), 2):
        parts[number] = re.sub(r"(\[[^\]\n]*\]\()([^)\n]+)(\))", replace, parts[number])
    result = "".join(parts).encode("utf-8")
    return (b"\xef\xbb\xbf" if raw.startswith(b"\xef\xbb\xbf") else b"") + result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    moves = [ROOT / "docs/current" / name for name in HISTORY]
    moves += [ROOT / "docs/prototypes" / name for name in OLD_HTML]
    snapshots = [ROOT / name for name in SNAPSHOTS]
    move_map = {path.resolve(): target_for(path).resolve() for path in moves}
    history_map = move_map | {path.resolve(): target_for(path).resolve() for path in snapshots}
    for source, destination in history_map.items():
        if not source.is_relative_to(ROOT) or not destination.is_relative_to(DEST):
            raise ValueError(f"Path outside archive scope: {source} -> {destination}")
        if not source.is_file() or destination.exists():
            raise ValueError(f"Missing source or existing target: {source} -> {destination}")
    plan = [{"action": "move" if path in move_map else "snapshot",
             "source": path.relative_to(ROOT).as_posix(),
             "destination": destination.relative_to(ROOT).as_posix()}
            for path, destination in history_map.items()]
    if not args.apply:
        print(json.dumps({"root": str(ROOT), "archive": str(DEST), "plan": plan},
                         ensure_ascii=False, indent=2))
        return
    if (DEST / "originals.zip").exists():
        raise ValueError("Archive already exists; refusing to overwrite original bytes")
    candidates = [path for path in (ROOT / "docs").rglob("*.md") if not path.is_relative_to(DEST)]
    candidates += [ROOT / name for name in ("README.md", "AGENTS.md", "CONTEXT.md")]
    originals, outputs = {}, {}
    for path in candidates:
        raw = path.read_bytes()
        destination = move_map.get(path.resolve(), path)
        mapping = history_map if path in moves or path.is_relative_to(ROOT / "docs/reviews") else move_map
        result = relocate(raw, path, destination, mapping)
        if result != raw or path in moves or path in snapshots:
            originals[path] = raw
        if result != raw:
            outputs[destination] = result
    for path in snapshots:
        raw = path.read_bytes()
        originals[path] = raw
        outputs[target_for(path)] = relocate(raw, path, target_for(path), history_map)
    for path in moves:
        originals.setdefault(path, path.read_bytes())
    # HTML has no external resources; one historical button has a relative return target.
    old_model = ROOT / "docs/prototypes/repomesh-model-settings-prototype.html"
    raw = originals[old_model]
    old = b"location.href='repomesh-project-entry-prototype.html?screen=repositories'"
    relative = os.path.relpath(ROOT / "docs/prototypes/repomesh-project-entry-prototype.html",
                              target_for(old_model).parent).replace("\\", "/")
    assert old in raw
    outputs[target_for(old_model)] = raw.replace(old, f"location.href='{relative}?screen=repositories'".encode())
    with zipfile.ZipFile(DEST / "originals.zip", "x", zipfile.ZIP_DEFLATED) as archive:
        for path, raw in originals.items():
            archive.writestr(path.relative_to(ROOT).as_posix(), raw)
    with zipfile.ZipFile(DEST / "originals.zip") as archive:
        assert archive.testzip() is None
        assert all(archive.read(path.relative_to(ROOT).as_posix()) == raw for path, raw in originals.items())
    for path in moves:
        destination = target_for(path)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(path), str(destination))
    for path, raw in outputs.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
    for entry in plan:
        source = ROOT / entry["source"]
        destination = ROOT / entry["destination"]
        entry["original_sha256"] = hashlib.sha256(originals[source]).hexdigest()
        entry["archive_sha256"] = hashlib.sha256(destination.read_bytes()).hexdigest()
    rewritten = [{"path": path.relative_to(ROOT).as_posix(),
                  "before_sha256": hashlib.sha256(raw).hexdigest(),
                  "after_sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                 for path, raw in originals.items() if path not in moves and path in outputs]
    (DEST / "manifest.json").write_text(json.dumps({
        "entries": plan, "navigation_rewrites": rewritten,
        "originals": "originals.zip", "raw_files_preserved": len(originals),
        "notes": "Only navigation links were rebased; original source bytes are retained in the zip. Evidence JSON and validation files were not rewritten."
    }, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"moved": len(moves), "snapshots": len(snapshots),
                      "raw_files_preserved": len(originals), "navigation_rewrites": len(rewritten)},
                     ensure_ascii=False))


if __name__ == "__main__":
    main()
