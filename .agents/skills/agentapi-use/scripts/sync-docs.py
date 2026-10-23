#!/usr/bin/env python3
"""Sync 七牛云 AgentAPI 官方文档到本 skill 的 references/ 目录。

数据来源：https://developer-doc.qiniu.com/products/agentapi/
做法：从站点 llms.txt 发现 AgentAPI 章节，逐页抓取 llms.mdx 的 Markdown 源，
做链接本地化与标题清理后写入 references/。

用法：
    python3 .claude/skills/agentapi-use/scripts/sync-docs.py           # 镜像全部文档
    python3 .claude/skills/agentapi-use/scripts/sync-docs.py --check   # 只比对差异，不写文件
    python3 .claude/skills/agentapi-use/scripts/sync-docs.py --only api,quick_start

依赖：无（仅标准库）。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.parse
import urllib.request
from collections import OrderedDict
from pathlib import Path

SITE = "https://developer-doc.qiniu.com"
PRODUCT_ROOT = "/products/agentapi"
PRODUCT_NAME = "AgentAPI"
USER_AGENT = "Mozilla/5.0 (compatible; agentapi-skill-doc-sync/1.0)"

SKILL_DIR = Path(__file__).resolve().parent.parent
REF_DIR = SKILL_DIR / "references"
MANIFEST = SKILL_DIR / "docs-manifest.json"

# 路由 slug 过于冗长时改为固定文件名
SLUG_OVERRIDES = {
    "/products/agentapi/product_dynamic/"
    "13626-qiniu-cloud-agent-api-is-officially-released-allowing-agent-to-accept-long-tasks":
        "official-release",
}

# 整章排除：计费文档不需要
EXCLUDE_PREFIXES = (PRODUCT_ROOT + "/buying_guide/",)

IMAGE_RE = re.compile(r"!\[([^\]]*)\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
LINK_RE = re.compile(r"(?<!!)\[([^\]]+)\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
HEADING_ANCHOR_RE = re.compile(r"^(#{1,6}\s+.+?)\s+\[#[^\]]+\]\s*$")
H1_RE = re.compile(r"^#\s+(.+?)(?:\s+\(([^)]*)\))?\s*$")


# --------------------------------------------------------------------------- #
# 抓取与发现
# --------------------------------------------------------------------------- #
def fetch(path: str, timeout: int = 30) -> str:
    url = SITE + urllib.parse.quote(path, safe="/:%?=&#")
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return resp.read().decode("utf-8", "replace").lstrip("﻿")
        except Exception:
            if attempt == 2:
                raise
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError("unreachable")


def discover_pages() -> list[dict]:
    """解析 llms.txt 中 AgentAPI 章节，保留文档顺序与分组。"""
    lines = fetch("/llms.txt", timeout=60).splitlines()
    start = None
    for i, line in enumerate(lines):
        if re.match(r"^\s{2}- " + re.escape(PRODUCT_NAME) + r"\s*$", line):
            start = i + 1
            break
    if start is None:
        sys.exit("llms.txt 中找不到 AgentAPI 章节")

    section_lines: list[str] = []
    for line in lines[start:]:
        if re.match(r"^\s{2}- \S", line):
            break
        section_lines.append(line)

    pages: list[dict] = []
    section = ""
    group = ""
    group_indent = 0
    for raw in section_lines:
        stripped = raw.strip()
        if not stripped:
            continue
        indent = len(raw) - len(raw.lstrip())
        match = re.match(r"^- \[([^\]]+)\]\(([^)]+)\)(?::\s*(.*))?$", stripped)
        if match:
            if indent <= group_indent:
                group = ""
                group_indent = 0
            route = match.group(2).split("#")[0].rstrip("/")
            if not route.startswith(PRODUCT_ROOT) \
                    or route.startswith(EXCLUDE_PREFIXES):
                continue
            pages.append({
                "route": route,
                "title": match.group(1).strip(),
                "description": (match.group(3) or "").strip(),
                "section": section,
                "group": group,
                "url": SITE + urllib.parse.quote(route, safe="/"),
            })
        elif stripped.startswith("- "):
            label = stripped[2:].strip()
            if indent <= 4:
                section = label
                group = ""
                group_indent = 0
            else:
                group = label
                group_indent = indent
    return pages


# --------------------------------------------------------------------------- #
# 本地路径
# --------------------------------------------------------------------------- #
def page_path(route: str) -> Path:
    parts = route[len(PRODUCT_ROOT):].strip("/").split("/")
    dirs = [strip_prefix(parts[0])]
    if len(parts) > 2:
        dirs.extend(strip_prefix(part) for part in parts[1:-1])
    last = parts[-1]
    if route in SLUG_OVERRIDES:
        name = SLUG_OVERRIDES[route]
    else:
        name = re.sub(r"^\d+-", "", last)
        name = re.sub(r"^agentapi-", "", name)
        # 嵌套章节的文件名去掉父级前缀，如 agent-configuration-runtime-and-models
        if len(parts) > 2:
            parent = strip_prefix(parts[-2])
            if name.startswith(parent + "-"):
                name = name[len(parent) + 1:]
    return REF_DIR.joinpath(*dirs, name + ".md")


def strip_prefix(name: str) -> str:
    """目录名去掉 product 前缀并统一用短横线。"""
    return re.sub(r"^agentapi-", "", name.replace("_", "-"))


# --------------------------------------------------------------------------- #
# Markdown 本地化
# --------------------------------------------------------------------------- #
def resolve_image(src: str, route: str) -> str:
    if src.startswith(("http://", "https://", "data:")):
        return src
    if src.startswith("/"):
        return SITE + src
    return SITE + urllib.parse.urljoin(route + "/", src)


def resolve_link(href: str, out_path: Path, route_map: dict[str, Path]) -> str:
    if href.startswith(("http://", "https://", "mailto:", "#")):
        return href
    if not href.startswith("/"):
        return href
    path = href.partition("#")[0].rstrip("/")
    target = route_map.get(path)
    if target is None:
        return SITE + href
    rel = os.path.relpath(target, out_path.parent).replace(os.sep, "/")
    return rel


def localize(route: str, raw: str, out_path: Path,
             route_map: dict[str, Path]) -> tuple[str, str]:
    lines = raw.splitlines()
    title = ""
    if lines and lines[0].startswith("# "):
        match = H1_RE.match(lines[0])
        if match:
            title = match.group(1).strip()
            lines[0] = "# " + title

    out: list[str] = []
    fence: int | None = None
    for line in lines:
        stripped = line.lstrip()
        fence_match = re.match(r"^(`{3,})", stripped)
        if fence is None:
            if fence_match:
                fence = len(fence_match.group(1))
                out.append(line)
                continue
        else:
            if fence_match and len(fence_match.group(1)) >= fence \
                    and not stripped[fence:].strip():
                fence = None
            out.append(line)
            continue

        line = HEADING_ANCHOR_RE.sub(r"\1", line)
        line = IMAGE_RE.sub(
            lambda m: f"![{m.group(1)}]({resolve_image(m.group(2), route)})", line)
        line = LINK_RE.sub(
            lambda m: f"[{m.group(1)}]({resolve_link(m.group(2), out_path, route_map)})",
            line)
        out.append(line)

    return title, "\n".join(out).rstrip() + "\n"


def render_page(route: str, title: str, body: str) -> str:
    header = (f"<!-- 来源：{SITE}{urllib.parse.quote(route, safe='/')} "
              f"| 由 sync-docs.py 自动生成，请勿手改 -->\n\n")
    if not body.startswith("# "):
        body = f"# {title}\n\n" + body
    return header + body


# --------------------------------------------------------------------------- #
# 索引与清单
# --------------------------------------------------------------------------- #
def write_doc_index(pages: list[dict]) -> None:
    groups: OrderedDict[str, OrderedDict[str, list[dict]]] = OrderedDict()
    for page in pages:
        section = page["section"] or "文档"
        groups.setdefault(section, OrderedDict()).setdefault(page["group"], []).append(page)

    lines = [
        "<!-- 由 sync-docs.py 自动生成，请勿手改 -->",
        "",
        "# 文档目录",
        "",
        f"共 {len(pages)} 篇，镜像自 {SITE}{PRODUCT_ROOT}/。"
        "接口端点速查见 [api-quick-reference.md](api-quick-reference.md)。",
        "",
    ]
    for section, sub_groups in groups.items():
        lines += [f"## {section}", ""]
        has_groups = any(group for group in sub_groups)
        for group, items in sub_groups.items():
            if group:
                lines += [f"### {group}", ""]
            elif has_groups:
                lines += ["### 其他", ""]
            lines += ["| 文档 | 本地文件 | 原文 |", "| --- | --- | --- |"]
            for page in items:
                rel = os.path.relpath(SKILL_DIR / page["file"], REF_DIR).replace(os.sep, "/")
                lines.append(f"| {page['title']} | [{rel}]({rel}) | {page['url']} |")
            lines.append("")
    (REF_DIR / "INDEX.md").write_text("\n".join(lines), encoding="utf-8")


def write_endpoint_index(pages: list[dict]) -> None:
    openapi = next((p for p in pages if p["route"].endswith("-agentapi-openapi")), None)
    if openapi is None:
        return
    path = SKILL_DIR / openapi["file"]
    if not path.exists():
        return
    rows: list[tuple[str, str, str]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        match = re.match(
            r"^\|\s*(GET|POST|PUT|PATCH|DELETE)\s*\|\s*`([^`]+)`\s*\|\s*(.*?)\s*\|\s*$",
            line)
        if match:
            rows.append((match.group(1), match.group(2), match.group(3)))
    if not rows:
        return
    rel = os.path.relpath(path, REF_DIR).replace(os.sep, "/")
    body = "\n".join([
        "<!-- 由 sync-docs.py 自动生成，请勿手改 -->",
        "",
        "# API 速查",
        "",
        f"共 {len(rows)} 个端点。服务地址按区域选择：华东 `https://agent.qiniuapi.com`，"
        "美西 `https://agent.sufy.com`。认证头 `x-api-key: <maas-api-key>`。",
        "完整请求与响应字段见 [OpenAPI 规范](" + rel + ")",
        "（[下载 OpenAPI YAML](https://dn-odum9helk.qbox.me/FvPJkqgApx2DcsC4EdR9Ym2mYv_t)）。",
        "",
        "| 方法 | 路径 | 说明 |",
        "| --- | --- | --- |",
        *[f"| `{method}` | `{endpoint}` | {desc} |" for method, endpoint, desc in rows],
        "",
    ])
    (REF_DIR / "api-quick-reference.md").write_text(body, encoding="utf-8")


def write_manifest(pages: list[dict]) -> None:
    data = {
        "source": SITE + PRODUCT_ROOT + "/",
        "syncedAt": time.strftime("%Y-%m-%d"),
        "pages": [
            {
                "route": page["route"],
                "title": page["title"],
                "file": page["file"],
                "url": page["url"],
                "section": page["section"],
                "group": page["group"],
                "description": page["description"],
            }
            for page in pages
        ],
    }
    MANIFEST.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n",
                        encoding="utf-8")


def prune(known: set[Path]) -> None:
    for path in sorted(REF_DIR.rglob("*.md")):
        if path.name in ("INDEX.md", "api-quick-reference.md"):
            continue
        if path not in known:
            path.unlink()
            print(f"  - 移除下线页面 {path.relative_to(SKILL_DIR)}")
    for path in sorted(REF_DIR.rglob("*"), reverse=True):
        if path.is_dir() and not any(path.iterdir()):
            path.rmdir()


# --------------------------------------------------------------------------- #
# 主流程
# --------------------------------------------------------------------------- #
def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="只输出差异，不写文件")
    parser.add_argument("--only", default="", help="只同步指定章节，如 api,quick_start")
    args = parser.parse_args()

    print("发现文档目录 …")
    pages = discover_pages()
    if args.only:
        prefixes = tuple(PRODUCT_ROOT + "/" + p.strip().strip("/") + "/"
                         for p in args.only.split(","))
        pages = [p for p in pages if p["route"].startswith(prefixes)]
    print(f"共 {len(pages)} 个页面")

    route_map = {page["route"]: page_path(page["route"]) for page in pages}
    changed = 0
    for page in pages:
        out = route_map[page["route"]]
        try:
            raw = fetch("/llms.mdx/docs" + page["route"] + "/content.md")
        except Exception as exc:  # noqa: BLE001
            print(f"  ! {page['route']} 抓取失败：{exc}")
            if out.exists():
                page["file"] = str(out.relative_to(SKILL_DIR))
            continue
        title, body = localize(page["route"], raw, out, route_map)
        if title:
            page["title"] = title
        content = render_page(page["route"], page["title"], body)
        page["file"] = str(out.relative_to(SKILL_DIR))
        old = out.read_text(encoding="utf-8") if out.exists() else None
        if old != content:
            changed += 1
            if not args.check:
                out.parent.mkdir(parents=True, exist_ok=True)
                out.write_text(content, encoding="utf-8")
            print(f"  {'~' if old is not None else '+'} {page['route']}")

    if args.check:
        print(f"检查完成：{changed} 个页面有更新")
        return 0

    # 局部同步只更新页面，不改动全量索引，避免清单缺页
    if not args.only:
        write_doc_index(pages)
        write_endpoint_index(pages)
        write_manifest(pages)
        prune({page_path(page["route"]) for page in pages})

    print(f"完成：{len(pages)} 个页面，{changed} 个有更新")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
