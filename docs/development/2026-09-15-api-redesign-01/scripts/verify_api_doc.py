#!/usr/bin/env python3
"""Static checks that tie docs/current/api-design.md to the adopted database plan.

Run from the repository root:

    python3 docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py

Exit code 0 means every check passed. The script reads files only.
"""

from __future__ import annotations

import html
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
DB_PLAN = ROOT / "docs" / "RepoMesh_Go版数据库重构方案.html"
API_DOC = ROOT / "docs" / "current" / "api-design.md"
API_PREFIX = "/api/v1"
ADR_COUNT = 23
MAPPING_HEADING = "## 附录 A：表与资源映射"
LINK_CHECKED = [
    ROOT / "AGENTS.md",
    ROOT / "docs" / "README.md",
    ROOT / "docs" / "current" / "README.md",
    ROOT / "docs" / "current" / "HANDOFF.md",
    ROOT / "docs" / "current" / "api-design.md",
    ROOT / "docs" / "archive" / "2026-09-15-api-database-catalog" / "README.md",
    ROOT / "docs" / "development" / "2026-09-15-api-redesign-01" / "README.md",
]
LIVE_DOC_DIRS = [ROOT / "docs" / "current", ROOT / "docs" / "plan", ROOT / "docs" / "adr"]
LIVE_DOC_FILES = [ROOT / "AGENTS.md", ROOT / "README.md", ROOT / "docs" / "README.md"]

failures: list[str] = []


def fail(msg: str) -> None:
    failures.append(msg)


def plan_tables() -> list[str]:
    src = DB_PLAN.read_text(encoding="utf-8")
    return re.findall(r'<div class="tbl" id="t_([a-z_]+)">', src)


def plan_table_fields(src: str, table: str) -> list[str]:
    start = src.index(f'id="t_{table}"')
    end = src.find('<div class="tbl"', start + 1)
    block = src[start : end if end != -1 else len(src)]
    return re.findall(r'<td class="cn">([a-z_]+)</td>', block)


def section(doc: str, heading: str) -> str:
    start = doc.find(heading)
    if start == -1:
        fail(f"api-design.md 缺少章节 {heading!r}")
        return ""
    rest = doc[start + len(heading) :]
    nxt = re.search(r"^## ", rest, flags=re.M)
    return rest[: nxt.start()] if nxt else rest


def check_mapping(doc: str, tables: list[str]) -> set[str]:
    body = section(doc, MAPPING_HEADING)
    rows = [line for line in body.splitlines() if line.startswith("|")]
    mapped: dict[str, str] = {}
    resources: set[str] = set()
    for line in rows[2:]:
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 3:
            continue
        m = re.fullmatch(r"`([a-z_]+)`", cells[0])
        if not m:
            continue
        mapped[m.group(1)] = cells[1]
        for res in re.findall(r"`" + re.escape(API_PREFIX) + r"/([a-z\-]+)", cells[1]):
            resources.add(res)
    missing = [t for t in tables if t not in mapped]
    extra = [t for t in mapped if t not in tables]
    if missing:
        fail(f"映射表缺少方案中的表：{missing}")
    if extra:
        fail(f"映射表出现方案之外的表：{extra}")
    for t, cell in mapped.items():
        if "内部表" not in cell and API_PREFIX not in cell:
            fail(f"表 `{t}` 的映射既没有端点也没有标为内部表：{cell!r}")
    return resources


def check_endpoints(doc: str, resources: set[str]) -> None:
    body_endpoints = set(
        re.findall(
            r"`(GET|POST|PUT|PATCH|DELETE) (" + re.escape(API_PREFIX) + r"/[^`\s]+)`",
            doc,
        )
    )
    if len(body_endpoints) < 40:
        fail(f"正文端点数量只有 {len(body_endpoints)}，低于 40，疑似格式不符")
    for _method, path in sorted(body_endpoints):
        first = path[len(API_PREFIX) + 1 :].split("/")[0].split("?")[0]
        if first not in resources:
            fail(f"端点 {path} 的顶层资源 `{first}` 未出现在映射表的端点列中")


def check_adr_citations(doc: str) -> None:
    for n in range(1, ADR_COUNT + 1):
        tag = f"ADR-{n:04d}"
        if tag not in doc:
            fail(f"api-design.md 未引用 {tag}")


def check_fields(doc: str, tables: list[str]) -> None:
    src = DB_PLAN.read_text(encoding="utf-8")
    body = doc
    for t in tables:
        fields = plan_table_fields(src, t)
        if not fields:
            fail(f"无法从方案中解析 `{t}` 的字段")
            continue
        heading = re.search(r"^###+ .*`" + t + r"`.*$", body, flags=re.M)
        if not heading:
            continue
        rest = body[heading.end() :]
        nxt = re.search(r"^###? ", rest, flags=re.M)
        block = rest[: nxt.start()] if nxt else rest
        absent = [f for f in fields if f"`{f}`" not in block and f not in ("id",)]
        if absent:
            fail(f"资源 `{t}` 的章节未提到方案字段：{absent}")


def check_links() -> None:
    for path in LINK_CHECKED:
        if not path.exists():
            fail(f"应存在的文件缺失：{path.relative_to(ROOT)}")
            continue
        text = path.read_text(encoding="utf-8")
        for target in re.findall(r"\]\(([^)\s]+)\)", text):
            if target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            rel = target.split("#", 1)[0]
            if not rel:
                continue
            if not (path.parent / rel).exists():
                fail(f"{path.relative_to(ROOT)} 的链接不可解析：{target}")


def check_no_live_reference() -> None:
    files: list[Path] = list(LIVE_DOC_FILES)
    for d in LIVE_DOC_DIRS:
        files.extend(d.rglob("*.md"))
    for f in files:
        if not f.exists():
            continue
        if re.search(r"\]\([^)]*api-database/", f.read_text(encoding="utf-8")):
            fail(f"现行文档仍链接已删除目录：{f.relative_to(ROOT)}")
    if (ROOT / "docs" / "api-database").exists():
        fail("docs/api-database 目录仍然存在")


def main() -> int:
    tables = plan_tables()
    if len(tables) != 44:
        fail(f"方案中解析到 {len(tables)} 张表，期望 44")
    if not API_DOC.exists():
        fail("docs/current/api-design.md 不存在")
        doc = ""
    else:
        doc = API_DOC.read_text(encoding="utf-8")
    if doc:
        resources = check_mapping(doc, tables)
        check_endpoints(doc, resources)
        check_adr_citations(doc)
        check_fields(doc, tables)
    check_links()
    check_no_live_reference()
    print(f"方案表数 {len(tables)}；检查项 6；失败 {len(failures)}")
    for msg in failures:
        print("FAIL", msg)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
