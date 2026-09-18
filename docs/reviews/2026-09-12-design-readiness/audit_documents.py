"""Read-only structural checks for the three requested design directories."""

import hashlib
import json
import os
import re
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
DIRECTORIES = ("docs/adr", "docs/current", "docs/prototypes",
               "docs/agentteams-survey-2026-09-07")

PAGE_FILES = set("""conversation-issue-separation-design conversation-message-target-design
repository-picker-design project-configuration-design prototype-walkthrough-design
login-recovery-page-design model-connection-settings-design model-key-save-design
model-project-apply-design issue-overview-minimal-design first-batch-recovery-design
first-batch-complete-review first-development-todo HANDOFF-PAGE-API-DESIGN
page-interface-prototype draft-issue-and-room-entry-design project-first-entry-design
NEXT-SESSION-PROMPT""".split())
API_FILES = set("""issue-page-create-api-contract first-batch-browser-api-contract
conversation-message-clarification-api-contract authentication-browser-api-draft
model-settings-browser-api-draft backend-first-batch-persistence
backend-message-clarification-design backend-first-development-access-draft
backend-first-batch-sources-draft backend-model-operations-draft
draft-conversation-backend-design manager-create-issue-tool-design
HANDOFF-BACKEND-DESIGN HANDOFF-BACKEND-DESIGN-2026-09-11 NEXT-BACKEND-SESSION-PROMPT""".split())
SYSTEM_FILES = set("""architecture-design-v1 technology-selection graph-loop-design
team-execution-policy verification-node-design integration-environment-design
draft-pr-review-design changeset-design changeset-structure changeset-design-discussion
skill-engineering-design issue-creation-repository-analysis agentteams-validation-plan""".split())


def write_coverage(inventory):
    lines = ["# 审查文件覆盖", "",
             "审查对象以 source-baseline.json 中的工作树字节为准。所有文件均进行了结构扫描；下表注明语义审查责任和阅读方式。",
             "现行协议由各专项审读，原型重复区块按比对及差异审读处理。历史通信日志进行了全文件结构索引和采用来源定点追溯，未声称逐条独立重审全部历史对话。",
             "上游专项的覆盖、版本与未运行范围见各报告。此表不是业务验收通过清单。", "",
             "| 文件 | 行数 | 语义审查责任/方式 |", "| --- | ---: | --- |"]
    for item in inventory:
        path = Path(item["path"])
        if item["path"].startswith("docs/adr/") or path.stem in SYSTEM_FILES:
            owner = "系统架构专项审读"
        elif item["path"].startswith("docs/prototypes/"):
            owner = "页面专项；完整源文件覆盖，共用区块比对及新增差异审读"
        elif item["path"].startswith("docs/agentteams-survey-"):
            owner = "API/后端审读及独立上游对照；HTML提取完整正文"
        elif path.stem in PAGE_FILES:
            owner = "页面专项审读"
        elif path.stem in API_FILES:
            owner = "API/后端专项审读"
        elif path.name.startswith("design-communication-"):
            owner = "主审；全文件主题/决定索引，关键采用原文追溯"
        elif path.name == "scaffold-source-inventory.md":
            owner = "主审；历史清单结构读取，不继承为本轮实验覆盖"
        else:
            owner = "主审；入口、历史审计、采用来源和完成度核对"
        link = os.path.relpath(ROOT / path, OUT).replace("\\", "/")
        lines.append(f"| [{item['path']}]({link}) | {item['lines']} | {owner} |")
    (OUT / "coverage.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def inspect():
    inventory, links, examples, log_sections = [], [], [], []
    for directory in DIRECTORIES:
        for path in sorted((ROOT / directory).rglob("*")):
            if not path.is_file():
                continue
            raw = path.read_bytes()
            source = raw.decode("utf-8-sig")
            rel = path.relative_to(ROOT).as_posix()
            inventory.append({"path": rel, "bytes": len(raw),
                              "lines": len(source.splitlines()),
                              "sha256": hashlib.sha256(raw).hexdigest()})
            if path.suffix == ".md":
                prose = re.sub(r"^```[^\n]*\n.*?^```[^\n]*$",
                               lambda m: re.sub(r"[^\n]", " ", m.group()),
                               source, flags=re.M | re.S)
                for match in re.finditer(r"\[[^\]\n]*\]\(([^)\n]+)\)", prose):
                    target = match.group(1).strip().strip("<>")
                    if re.match(r"(?:https?://|mailto:|codex:|#|[A-Za-z]:[/\\])", target):
                        continue
                    file_part = unquote(target.split("#", 1)[0])
                    if not file_part:
                        continue
                    resolved = (path.parent / file_part).resolve()
                    links.append({"source": rel,
                                  "line": source.count("\n", 0, match.start()) + 1,
                                  "target": target, "exists": resolved.exists()})
                for match in re.finditer(r"^```json\s*\n(.*?)^```", source, re.M | re.S):
                    error = None
                    try:
                        json.loads(match.group(1))
                    except (ValueError, TypeError) as exc:
                        error = str(exc)
                    examples.append({"source": rel,
                                     "line": source.count("\n", 0, match.start()) + 1,
                                     "error": error})
            if path.name.startswith("design-communication-"):
                for number, line in enumerate(source.splitlines(), 1):
                    if line.startswith("#") or re.search(r"用户.{0,12}(?:原话|回复|要求|同意|采用|确认)|待用户|无剩余.*分歧", line):
                        log_sections.append({"source": rel, "line": number, "text": line})
    result = {"directories": DIRECTORIES, "inventory": inventory,
              "local_links": {"checked": len(links), "missing": [x for x in links if not x["exists"]]},
              "json_examples": {"checked": len(examples), "invalid": [x for x in examples if x["error"]]},
              "communication_decision_lines": log_sections}
    baseline_path = OUT / "source-baseline.json"
    if baseline_path.exists():
        baseline = json.loads(baseline_path.read_text(encoding="utf-8"))
    else:
        previous_path = OUT / "structural-checks.json"
        baseline = (json.loads(previous_path.read_text(encoding="utf-8"))["inventory"]
                    if previous_path.exists() else inventory)
        existing = {x["path"] for x in baseline}
        baseline += [x for x in inventory if x["path"] not in existing]
        baseline_path.write_text(json.dumps(baseline, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    current = {x["path"]: x["sha256"] for x in inventory}
    result["source_changes_since_baseline"] = [x["path"] for x in baseline
                                              if current.get(x["path"]) != x["sha256"]]
    destination = OUT / "structural-checks.json"
    destination.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    write_coverage(inventory)
    print(json.dumps({"files": len(inventory), "local_links": result["local_links"],
                      "json_examples": result["json_examples"],
                      "decision_lines": len(log_sections),
                      "source_changes_since_baseline": result["source_changes_since_baseline"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    inspect()
