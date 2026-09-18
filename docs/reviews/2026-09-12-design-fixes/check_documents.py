"""Check revised design documents without rewriting the original audit evidence."""

import hashlib
import json
import re
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
SOURCE_DIRS = ("docs/adr", "docs/current", "docs/prototypes",
               "docs/agentteams-survey-2026-09-07")


def main():
    baseline_path = OUT.parent / "2026-09-12-design-readiness/source-baseline.json"
    baseline = {item["path"]: item["sha256"]
                for item in json.loads(baseline_path.read_text(encoding="utf-8"))}
    inventory, missing, invalid, whitespace = [], [], [], []
    link_count = example_count = 0
    files = [path for directory in SOURCE_DIRS for path in (ROOT / directory).rglob("*")
             if path.is_file()]
    files.extend(OUT.glob("*.md"))
    for path in sorted(files):
        raw = path.read_bytes()
        source = raw.decode("utf-8-sig")
        rel = path.relative_to(ROOT).as_posix()
        digest = hashlib.sha256(raw).hexdigest()
        inventory.append({"path": rel, "sha256": digest})
        if path.suffix != ".md":
            continue
        prose = re.sub(r"^```[^\n]*\n.*?^```[^\n]*$", "", source,
                       flags=re.M | re.S)
        for match in re.finditer(r"\[[^\]\n]*\]\(([^)\n]+)\)", prose):
            target = match.group(1).strip().strip("<>")
            if re.match(r"(?:https?://|mailto:|codex:|#)", target):
                continue
            file_part = unquote(target.split("#", 1)[0])
            if not file_part:
                continue
            link_count += 1
            if not (path.parent / file_part).resolve().exists():
                missing.append({"source": rel, "target": target})
        for match in re.finditer(r"^```json\s*\n(.*?)^```", source, re.M | re.S):
            example_count += 1
            try:
                json.loads(match.group(1))
            except ValueError as exc:
                invalid.append({"source": rel, "error": str(exc)})
        if baseline.get(rel) != digest:
            whitespace.extend({"source": rel, "line": number}
                              for number, line in enumerate(source.splitlines(), 1)
                              if line.rstrip() != line)
    current = {item["path"]: item["sha256"] for item in inventory}
    result = {
        "files": len(inventory),
        "local_links": {"checked": link_count, "missing": missing},
        "json_examples": {"checked": example_count, "invalid": invalid},
        "changed_document_trailing_whitespace": whitespace,
        "changed_since_audit": [name for name, digest in baseline.items()
                                if current.get(name) != digest],
        "new_sources": [name for name in current if name not in baseline
                        and any(name.startswith(directory + "/") for directory in SOURCE_DIRS)],
        "inventory": inventory,
        "limitations": ["Local file existence only; anchors and remote URLs not checked.",
                        "JSON syntax only; no business schema or runtime validation."]}
    (OUT / "document-checks.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({key: value for key, value in result.items() if key != "inventory"},
                     ensure_ascii=False, indent=2))
    return bool(missing or invalid or whitespace)


if __name__ == "__main__":
    raise SystemExit(main())
