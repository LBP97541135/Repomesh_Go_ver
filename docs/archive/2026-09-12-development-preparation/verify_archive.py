"""Validate archive preservation, navigation changes and the generated prototype."""
import base64
import hashlib
import json
import re
import runpy
import subprocess
import zipfile
from pathlib import Path
from urllib.parse import unquote

DEST = Path(__file__).resolve().parent
ROOT = DEST.parents[2]


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def links(path, raw):
    source = raw.decode("utf-8-sig")
    source = re.sub(r"^```[^\n]*\n.*?^```[^\n]*$", "", source, flags=re.M | re.S)
    for match in re.finditer(r"\[[^\]\n]*\]\(([^)\n]+)\)", source):
        target = match.group(1).strip().strip("<>")
        if re.match(r"(?:https?://|mailto:|codex:|#)", target):
            continue
        file_part = unquote(target.split("#", 1)[0])
        file_part = re.sub(r":\d+$", "", file_part)
        if not file_part:
            continue
        resolved = (path.parent / file_part).resolve()
        if resolved.is_relative_to(ROOT):
            yield resolved


def main():
    manifest = json.loads((DEST / "manifest.json").read_text(encoding="utf-8"))
    # Make this run's output link resolvable on the first run too.
    (DEST / "verification.json").write_text('{"status":"checking"}\n', encoding="utf-8")
    with zipfile.ZipFile(DEST / "originals.zip") as archive:
        assert archive.testzip() is None
        originals = {ROOT / name: archive.read(name) for name in archive.namelist()}
    preservation_errors = []
    inverse, moved = {}, set()
    for entry in manifest["entries"]:
        source, destination = ROOT / entry["source"], ROOT / entry["destination"]
        inverse[destination] = source
        if entry["action"] == "move":
            moved.add(source)
            if source.exists():
                preservation_errors.append(f"Moved source still exists: {source}")
        if sha(originals[source]) != entry["original_sha256"]:
            preservation_errors.append(f"Original hash differs: {source}")
        if not destination.exists() or sha(destination.read_bytes()) != entry["archive_sha256"]:
            preservation_errors.append(f"Archive hash differs: {destination}")
    before_paths = {p for p in (ROOT / "docs").rglob("*.md") if not p.is_relative_to(DEST)}
    before_paths |= {p for p in originals if p.suffix == ".md"}
    before_paths |= {ROOT / name for name in ("README.md", "AGENTS.md", "CONTEXT.md")}
    before_missing = set()
    for path in before_paths:
        raw = originals[path] if path in originals else path.read_bytes()
        for target in links(path, raw):
            existed = target in moved or (not target.is_relative_to(DEST) and target.exists())
            if not existed:
                before_missing.add((path, target))
    after_paths = list((ROOT / "docs").rglob("*.md"))
    after_paths += [ROOT / name for name in ("README.md", "AGENTS.md", "CONTEXT.md")]
    after_missing, introduced = [], []
    checked = 0
    for path in after_paths:
        for target in links(path, path.read_bytes()):
            checked += 1
            if not target.exists():
                item = {"source": path.relative_to(ROOT).as_posix(),
                        "target": target.relative_to(ROOT).as_posix()}
                after_missing.append(item)
                if (inverse.get(path, path), inverse.get(target, target)) not in before_missing:
                    introduced.append(item)
    prototype = ROOT / "docs/prototypes"
    assembly = runpy.run_path(str(prototype / "assemble-prototypes.py"))
    encoded = {key: assembly["build_source"](key, prototype / name)
               for key, name in assembly["SOURCES"].items()}
    original_html = {path.name: sha(path.read_bytes()) for path in prototype.glob("*.html")
                     if path.name != "index.html"}
    shell = (prototype / "joined-preview-shell.html.template").read_text(encoding="utf-8")
    expected = shell.replace("__EMBEDDED_SOURCES__", json.dumps(encoded)).replace(
        "__SOURCE_HASHES__", json.dumps(original_html))
    generated_matches = expected == (prototype / "index.html").read_text(encoding="utf-8")
    catalog = re.findall(r"\['[^']*','([^']+\.html)'", shell)
    catalog_missing = [name for name in catalog if not (prototype / name).resolve().is_file()]
    prior = json.loads((ROOT / "docs/reviews/2026-09-12-design-readiness/source-baseline.json").read_text(encoding="utf-8"))
    original_hashes = {Path(item["path"]).name: item["sha256"] for item in prior
                       if item["path"].startswith("docs/prototypes/") and item["path"].endswith(".html")}
    changed_kept_html = [name for name, digest in original_html.items() if original_hashes[name] != digest]
    script_units = []
    html_paths = list(prototype.glob("*.html")) + list((DEST / "docs/prototypes").glob("*.html"))
    for path in html_paths:
        for number, body in enumerate(re.findall(r"<script\b[^>]*>(.*?)</script>", path.read_text(encoding="utf-8-sig"), re.S | re.I)):
            script_units.append({"name": path.name + ":" + str(number), "body": body})
    for key, content in encoded.items():
        html = base64.b64decode(content).decode("utf-8")
        for number, body in enumerate(re.findall(r"<script\b[^>]*>(.*?)</script>", html, re.S | re.I)):
            script_units.append({"name": "embedded-" + key + ":" + str(number), "body": body})
    script_units += [{"name": path.name, "body": path.read_text(encoding="utf-8")}
                     for path in prototype.glob("*.prototype.js")]
    checker = "const vm=require('node:vm');let data='';process.stdin.setEncoding('utf8');process.stdin.on('data',x=>data+=x);process.stdin.on('end',()=>{const units=JSON.parse(data),errors=[];for(const s of units){try{new vm.Script(s.body,{filename:s.name})}catch(e){errors.push(String(e))}}process.stdout.write(JSON.stringify({units:units.length,errors}));process.exitCode=errors.length?1:0});"
    syntax = subprocess.run(["node", "-e", checker], input=json.dumps(script_units),
                            capture_output=True, text=True, encoding="utf-8", check=False)
    syntax_result = json.loads(syntax.stdout) if syntax.stdout else {"errors": [syntax.stderr]}
    json_errors, example_count = [], 0
    for path in list((ROOT / "docs/current").glob("*.md")) + list((ROOT / "docs/adr").glob("*.md")):
        for match in re.finditer(r"^```json\s*\n(.*?)^```", path.read_text(encoding="utf-8-sig"), re.M | re.S):
            example_count += 1
            try:
                json.loads(match.group(1))
            except ValueError as exc:
                json_errors.append({"path": str(path), "error": str(exc)})
    result = {"moved_files": len(moved), "snapshots": len(inverse) - len(moved),
              "raw_original_files": len(originals), "preservation_errors": preservation_errors,
              "current_markdown_files": len(list((ROOT / "docs/current").glob("*.md"))),
              "local_links_checked": checked, "existing_missing_links": after_missing,
              "introduced_missing_links": introduced, "prototype_families": len(encoded),
              "current_original_html": len(original_html), "changed_kept_html": changed_kept_html,
              "catalog_entries": len(catalog), "catalog_missing": catalog_missing,
              "generated_index_matches": generated_matches,
              "javascript_syntax": syntax_result,
              "current_json_examples": example_count, "invalid_json_examples": json_errors,
              "limitations": "Local file links only, not anchors or remote URLs; generation and JSON syntax do not validate business behavior."}
    (DEST / "verification.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    summary = {key: value for key, value in result.items() if key != "existing_missing_links"}
    summary["preexisting_missing_link_occurrences"] = len(after_missing) - len(introduced)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    assert not preservation_errors and not introduced and not catalog_missing
    assert not changed_kept_html and generated_matches and not json_errors
    assert syntax.returncode == 0 and not syntax_result["errors"]


if __name__ == "__main__":
    main()
