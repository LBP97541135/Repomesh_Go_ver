#!/usr/bin/env python3
"""verify_routes: 代码路由 ↔ docs/current/api-design.md 防漂移校验。

用法：python scripts/verify_routes.py
从 internal/ 与 cmd/ 的 Go 源码提取全部 mux 路由（HandleFunc / route() 调用），
逐一校验其已登记进 docs/current/api-design.md（附录 C 或正文）。路径参数
{x} 双侧归一化后比对。有未登记路由则退出码 1。

能力边界：只校验"代码有、文档缺"这一个方向；文档标了"已实现"但代码不存在
（多登）与请求/响应契约语义漂移不设防，需人工评审兜底。
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROUTE_RE = re.compile(r'"((?:GET|POST|PUT|PATCH|DELETE) /[^"]*)"')

# 源码里用字符串拼接注册的路由：前缀 → 完整路径展开（手工维护，附证据行）。
DYNAMIC = {
    "POST /api/auth/github/": [  # internal/web/auth.go:59 + purpose (login|reconnect)
        "POST /api/auth/github/login",
        "POST /api/auth/github/reconnect",
    ],
}


def code_routes() -> set:
    routes = set()
    for sub in ("internal", "cmd"):
        base = ROOT / sub
        if not base.exists():
            continue
        for path in base.rglob("*.go"):
            text = path.read_text(encoding="utf-8", errors="replace")
            for match in ROUTE_RE.finditer(text):
                route = match.group(1)
                expansion = DYNAMIC.get(route)
                if expansion:
                    routes.update(expansion)
                else:
                    routes.add(route)
    return {normalize(r) for r in routes}


def normalize(route: str) -> str:
    return re.sub(r"\{[^}]+\}", "{param}", route)


def doc_text() -> str:
    return (ROOT / "docs" / "current" / "api-design.md").read_text(encoding="utf-8")


def registered_in_doc(route: str, doc: str) -> bool:
    pattern = re.escape(route).replace(re.escape("{param}"), r"\{[^}]+\}")
    return re.search(pattern, doc) is not None


def main() -> int:
    routes = code_routes()
    doc = doc_text()
    missing = sorted(r for r in routes if not registered_in_doc(r, doc))
    print(f"代码路由共 {len(routes)} 条")
    if missing:
        print(f"未登记进 docs/current/api-design.md 的路由 {len(missing)} 条：")
        for route in missing:
            print("  -", route)
        print("处理：在附录 C 登记去向，或在正文的资源小节中给出端点。")
        return 1
    print("全部路由已登记，代码与文档无漂移。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
