#!/usr/bin/env python3
"""Sync 灵矽（Xrobot）官方文档到本 skill 的 references/ 目录。

数据来源：https://linx.qiniu.com/docs/xrobot/（VitePress 站点）
做法：抓取每个文档页的 SSR HTML，抽取正文，转换成可读的 Markdown。

用法：
    python3 .claude/skills/linx-use/scripts/sync-docs.py           # 增量镜像全部文档
    python3 .claude/skills/linx-use/scripts/sync-docs.py --check   # 只比对差异，不写文件
    python3 .claude/skills/linx-use/scripts/sync-docs.py --only api,guide

依赖：beautifulsoup4（pip install beautifulsoup4）
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
from pathlib import Path

try:
    from bs4 import BeautifulSoup, Comment, NavigableString, Tag
except ImportError:  # pragma: no cover
    sys.exit("缺少依赖：pip install beautifulsoup4")

SITE = "https://linx.qiniu.com"
DOC_ROOT = "/docs/xrobot/"
USER_AGENT = "Mozilla/5.0 (compatible; linx-skill-doc-sync/1.0)"

SKILL_DIR = Path(__file__).resolve().parent.parent
REF_DIR = SKILL_DIR / "references"
MANIFEST = SKILL_DIR / "docs-manifest.json"

# 导航里没有、但线上可访问的页面
EXTRA_ROUTES = [
    "/docs/xrobot/api/user",
    "/docs/xrobot/guide/灵矽智能硬件智能体提示指南",
    "/docs/xrobot/mcp/example-music-pan",
]

# 章节首页没有 frontmatter 标题，用站点导航里的名字兜底
SECTION_TITLES = {
    "/docs/xrobot": "灵矽文档中心",
    "/docs/xrobot/guide": "最佳实践",
    "/docs/xrobot/api": "平台 API",
    "/docs/xrobot/mcp": "MCP 接入",
    "/docs/xrobot/platform": "设备接入协议",
    "/docs/xrobot/billing": "计费详情",
    "/docs/xrobot/faq": "常见问题",
}

# 组件使用样例，不属于产品文档
EXCLUDE_ROUTES = {"/docs/xrobot/api/example"}

# 整章排除：计费文档暂不需要
EXCLUDE_PREFIXES = (DOC_ROOT + "billing/",)

# 中文文件名 -> 英文文件名，便于工具引用
SLUG_OVERRIDES = {
    "灵矽智能硬件智能体提示指南": "hardware-agent-prompt-guide",
}

IGNORED_CLASSES = {
    "copy",
    "header-anchor",
    "expand-toggle-icon",
    "expand-btn",
    "visually-hidden",
    "sr-only",
    "lang",
}

RAW_DOCS = "https://raw.githubusercontent.com/qiniu/Xrobot-docs/main"


# --------------------------------------------------------------------------- #
# 抓取
# --------------------------------------------------------------------------- #
def fetch(path: str) -> str:
    url = SITE + urllib.parse.quote(path, safe="/:%?=&#")
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return resp.read().decode("utf-8", "replace")
        except Exception:
            if attempt == 2:
                raise
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError("unreachable")


def discover_routes(index_html: str) -> list[str]:
    soup = BeautifulSoup(index_html, "html.parser")
    routes = set()
    for a in soup.find_all("a", href=True):
        href = a["href"].split("#")[0]
        if (href.startswith(DOC_ROOT) and href not in EXCLUDE_ROUTES
                and not href.startswith(EXCLUDE_PREFIXES)):
            routes.add(href.rstrip("/") or DOC_ROOT)
    routes.update(EXTRA_ROUTES)
    routes.discard(DOC_ROOT + "api/example")
    return sorted(routes)


def out_path_for(route: str) -> Path:
    rel = route[len(DOC_ROOT):].strip("/")
    parts = [p for p in rel.split("/") if p]
    idx = "index"
    if not parts:
        parts = ["index"]
    elif parts[-1] == "api" or len(parts) == 1 and parts[0] in {
        "guide", "api", "mcp", "platform", "billing", "faq",
    }:
        parts.append("index")
    else:
        parts[-1] = SLUG_OVERRIDES.get(parts[-1], parts[-1])
    parts[-1] = parts[-1] + ".md"
    return REF_DIR.joinpath(*parts)


# --------------------------------------------------------------------------- #
# 文本工具
# --------------------------------------------------------------------------- #
def clean(text: str) -> str:
    return re.sub(r"[ \t]+", " ", text.replace("\u200b", "").replace("\xa0", " ")).strip()


def code_span(text: str) -> str:
    text = text.strip()
    if not text:
        return ""
    tick = "`"
    while tick in text:
        tick += "`"
    pad = " " if text.startswith("`") or text.endswith("`") else ""
    return f"{tick}{pad}{text}{pad}{tick}"


def escape_table(text: str) -> str:
    return text.replace("|", "\\|").replace("\n", "<br>")


# --------------------------------------------------------------------------- #
# HTML -> Markdown
# --------------------------------------------------------------------------- #
class Converter:
    def __init__(self, route: str, base: Path, route_map: dict[str, Path],
                 mermaid: list[str] | None = None):
        self.route = route
        self.base = base
        self.route_map = route_map
        self.endpoints: list[dict] = []
        self.mermaid = list(mermaid or [])

    # ---- inline ---------------------------------------------------------- #
    def inline(self, node) -> str:
        if isinstance(node, Comment):
            return ""
        if isinstance(node, NavigableString):
            return re.sub(r"\s+", " ", str(node))
        if not isinstance(node, Tag):
            return ""
        if node.name in ("script", "style", "svg", "button"):
            return ""
        classes = set(node.get("class") or [])
        if classes & IGNORED_CLASSES:
            return ""
        name = node.name
        if name == "code":
            return code_span(node.get_text())
        if name in ("strong", "b"):
            return f"**{self.inline_children(node).strip()}**"
        if name in ("em", "i"):
            return f"*{self.inline_children(node).strip()}*"
        if name in ("del", "s"):
            return f"~~{self.inline_children(node).strip()}~~"
        if name == "br":
            return "  \n"
        if name == "img":
            src = node.get("src", "")
            if not src or src.endswith(".svg"):
                return ""
            alt = node.get("alt", "")
            return f"![{alt}]({self.resolve_asset(src)})"
        if name == "a":
            text = self.inline_children(node).strip() or clean(node.get_text())
            href = node.get("href", "")
            if not href or href.startswith("#"):
                return text
            return f"[{text}]({self.resolve_link(href)})"
        if name == "kbd":
            return f"<kbd>{clean(node.get_text())}</kbd>"
        return self.inline_children(node)

    def inline_children(self, node) -> str:
        return "".join(self.inline(child) for child in node.children)

    def resolve_link(self, href: str) -> str:
        href = href.split("#")[0].strip()
        if not href:
            return ""
        if href.startswith(("http://", "https://", "mailto:")):
            return href
        absolute = urllib.parse.urljoin(SITE + self.route, href)
        path = urllib.parse.urlparse(absolute).path
        key = path[:-3] if path.endswith(".md") else path
        key = key.rstrip("/")
        if key in self.route_map:
            target = self.route_map[key].with_suffix(".md")
            rel = os.path.relpath(target, self.base)
            return rel.replace(os.sep, "/")
        if path.startswith(DOC_ROOT):
            return SITE + path
        return href

    def resolve_asset(self, src: str) -> str:
        if src.startswith("http"):
            return src
        if src.startswith("/"):
            return SITE + src
        # 图片资源在站点上按页面路径解析
        return SITE + urllib.parse.urljoin(self.route, src)

    # ---- blocks ---------------------------------------------------------- #
    def blocks(self, node) -> list[str]:
        out: list[str] = []
        for child in node.children:
            out.extend(self.block(child))
        return out

    def block(self, node) -> list[str]:
        if isinstance(node, Comment):
            return []
        if isinstance(node, NavigableString):
            text = clean(str(node))
            return [text] if text else []
        if not isinstance(node, Tag):
            return []

        name = node.name
        classes = set(node.get("class") or [])

        if name in ("script", "style", "svg", "button", "nav", "footer"):
            return []
        if classes & IGNORED_CLASSES:
            return []
        if name == "h1":
            return ["# " + clean(self.inline_children(node)), ""]
        if name in ("h2", "h3", "h4", "h5", "h6"):
            level = int(name[1])
            text = clean(self.inline_children(node))
            if not text:
                return []
            return ["#" * level + " " + text, ""]
        if name == "p":
            text = self.inline_children(node).strip()
            return [text, ""] if text else []
        if name == "hr":
            return ["---", ""]
        if name == "pre":
            return self.render_pre(node)
        if name == "table":
            return self.render_table(node)
        if name in ("ul", "ol"):
            return self.render_list(node) + [""]
        if name == "blockquote":
            inner = "\n".join(self.blocks(node)).strip()
            return ["> " + line if line else ">" for line in inner.splitlines()] + [""]
        if name == "img":
            text = self.inline(node)
            return [text, ""] if text else []
        if name == "details":
            summary = node.find("summary")
            head = clean(summary.get_text()) if summary else "详情"
            if summary:
                summary.extract()
            inner = self.blocks(node)
            return [f"**{head}**", ""] + inner
        if name == "div":
            if "api-endpoint" in classes:
                return self.render_endpoint(node)
            if "mermaid" in classes:
                # SSR 产物里 mermaid 容器是空的，图源从 GitHub 原始 Markdown 补齐
                source = node.get_text().strip()
                if not source and self.mermaid:
                    source = self.mermaid.pop(0)
                if not source:
                    return []
                return ["```mermaid", source.strip(), "```", ""]
            if "custom-block" in classes:
                return self.render_custom_block(node)
            if "vp-code-group" in classes:
                return self.render_code_group(node)
            return self.blocks(node)
        return self.blocks(node)

    def render_custom_block(self, node: Tag) -> list[str]:
        title = node.select_one(".custom-block-title")
        label = clean(title.get_text()) if title else ""
        if title:
            title.extract()
        inner = "\n".join(self.blocks(node)).strip()
        lines = ["> " + line if line else ">" for line in inner.splitlines()]
        if label:
            lines = [f"> **{label}**", ">"] + lines
        return lines + [""]

    def render_code_group(self, node: Tag) -> list[str]:
        out: list[str] = []
        for tab in node.select(".vp-code-group .tabs label, .tabs label"):
            pass
        for pre in node.find_all("pre"):
            out.extend(self.render_pre(pre))
        return out

    def render_pre(self, node: Tag) -> list[str]:
        code = node.find("code") or node
        lang = ""
        for cls in code.get("class") or []:
            if cls.startswith("language-"):
                lang = cls[len("language-"):]
        for cls in node.get("class") or []:
            if cls.startswith("language-"):
                lang = cls[len("language-"):]
        if "mermaid" in (node.get("class") or []):
            lang = "mermaid"
        raw = ""
        line_spans = code.find_all("span", class_="line")
        if line_spans:
            raw = "\n".join(clean_span_text(s) for s in line_spans)
        else:
            raw = code.get_text()
        raw = raw.rstrip("\n")
        if lang in ("text", "plain", "plaintext", "txt"):
            lang = ""
        return ["```" + lang, raw, "```", ""]

    def render_table(self, node: Tag) -> list[str]:
        rows: list[list[str]] = []
        for tr in node.find_all("tr"):
            cells = tr.find_all(["th", "td"])
            if cells:
                rows.append([escape_table(clean(self.inline_children(c))) for c in cells])
        if not rows:
            return []
        width = max(len(r) for r in rows)
        rows = [r + [""] * (width - len(r)) for r in rows]
        out = ["| " + " | ".join(rows[0]) + " |", "| " + " | ".join(["---"] * width) + " |"]
        for row in rows[1:]:
            out.append("| " + " | ".join(row) + " |")
        return out + [""]

    def render_list(self, node: Tag, depth: int = 0) -> list[str]:
        ordered = node.name == "ol"
        out: list[str] = []
        index = 1
        for li in node.find_all("li", recursive=False):
            marker = f"{index}. " if ordered else "- "
            inline_parts: list[str] = []
            nested: list[str] = []
            block_parts: list[str] = []
            has_block = False
            for child in li.children:
                if isinstance(child, Comment):
                    continue
                if isinstance(child, Tag) and child.name in ("ul", "ol"):
                    nested = self.render_list(child, depth + 1)
                elif isinstance(child, Tag) and child.name in (
                        "div", "h1", "h2", "h3", "h4", "h5", "h6",
                        "table", "pre", "blockquote", "details"):
                    has_block = True
                    block_parts.extend(self.block(child))
                elif isinstance(child, Tag) and child.name == "p":
                    inline_parts.append(self.inline_children(child).strip())
                else:
                    inline_parts.append(self.inline(child))
            text = clean(" ".join(p for p in inline_parts if p))
            if has_block and not text:
                out.extend(block_parts)
                out.extend(nested)
            elif not text and nested and not block_parts:
                # 纯容器 li（章节组件），直接展开
                out.extend(nested)
            else:
                out.append("  " * depth + marker + text)
                out.extend("  " * depth + line if line else line for line in nested)
            index += 1
        return out

    # ---- API 端点组件 ---------------------------------------------------- #
    def render_endpoint(self, node: Tag) -> list[str]:
        lines: list[str] = []
        title = node.select_one(".api-title")
        title_text = clean(title.get_text()) if title else ""
        if title_text:
            lines += ["### " + title_text, ""]
        desc = node.select_one(".api-description")
        if desc:
            text = clean(self.inline_children(desc))
            if text:
                lines += [text, ""]
        method_el = node.select_one(".method-badge")
        url_el = node.select_one(".api-url")
        if method_el and url_el:
            method = clean(method_el.get_text()).upper()
            url = clean(url_el.get_text())
            lines += [f"**`{method}`** `{url}`", ""]
            self.endpoints.append({"title": title_text, "method": method, "url": url,
                                   "route": self.route})

        for section in node.select(".info-section, .params-section, .headers-section, "
                                   ".example-section, .status-section"):
            header = section.select_one(".section-header")
            label = clean(header.get_text()) if header else ""
            if label:
                lines += ["#### " + label, ""]
            if section.select_one(".info-grid"):
                for item in section.select(".info-item"):
                    k = item.select_one(".label")
                    v = item.select_one(".info-value, .method-badge")
                    if k and v:
                        lines.append(f"- **{clean(k.get_text()).rstrip(':：')}**: "
                                     f"`{clean(v.get_text())}`")
                lines.append("")
            elif section.select_one(".table-header"):
                lines += self.render_grid_table(section)
            elif "example-section" in (section.get("class") or []):
                for pre in section.select("pre"):
                    lines += self.render_pre(pre)
            elif "status-section" in (section.get("class") or []):
                for row in section.select(".status-row"):
                    code = row.select_one(".status-code")
                    text = row.select_one(".status-description")
                    if code and text:
                        lines.append(f"- `{clean(code.get_text())}` "
                                     f"{clean(text.get_text())}")
                lines.append("")
        return lines

    def render_grid_table(self, section: Tag) -> list[str]:
        header = section.select_one(".table-header")
        headers = [clean(s.get_text()) for s in header.find_all("span", recursive=False)]
        rows: list[list[str]] = []
        for row in section.select(".table-row"):
            cells: list[str] = []
            for part in row.find_all(recursive=False):
                cls = set(part.get("class") or [])
                if cls & {"param-name-wrapper", "param-name"}:
                    name = part.select_one(".param-name") or part
                    cells.append(code_span(clean(name.get_text())))
                elif "param-type" in cls:
                    cells.append(clean(part.get_text()))
                elif "param-required" in cls:
                    cells.append(clean(part.get_text()))
                elif "param-in" in cls:
                    cells.append(clean(part.get_text()))
                elif "param-description" in cls:
                    cells.append(clean(part.get_text()))
                else:
                    text = clean(part.get_text())
                    if text:
                        cells.append(text)
            if cells:
                rows.append(cells)
        if not headers and not rows:
            return []
        width = max([len(headers)] + [len(r) for r in rows]) if (headers or rows) else 0
        headers = headers + [""] * (width - len(headers))
        rows = [r + [""] * (width - len(r)) for r in rows]
        if not headers or not any(headers):
            headers = ["参数" + str(i + 1) for i in range(width)]
        out = ["| " + " | ".join(escape_table(h) for h in headers) + " |",
               "| " + " | ".join(["---"] * width) + " |"]
        for r in rows:
            out.append("| " + " | ".join(escape_table(c) for c in r) + " |")
        return out + [""]

    # ---- 页面 ------------------------------------------------------------ #
    def page(self, html: str) -> tuple[str, str]:
        soup = BeautifulSoup(html, "html.parser")
        title = ""
        if soup.title:
            title = clean(soup.title.get_text()).split(" | ")[0]
        doc = soup.select_one(".vp-doc")
        if doc is None:
            return title, ""
        # 去掉锚点、复制按钮等
        for tag in doc.select(".header-anchor, .copy, .expand-btn, script, style"):
            tag.decompose()
        # 去掉正文里重复的 <h1>（站点标题已在页面 title）
        for h1 in doc.find_all("h1"):
            h1.decompose()
        lines = self.blocks(doc)
        md = dedupe_headings("\n".join(lines))
        return title, md.strip() + "\n"


def clean_span_text(span: Tag) -> str:
    text = span.get_text()
    return text.replace("\u200b", "")


def dedupe_headings(md: str) -> str:
    """组件标题与手写标题重复时，去掉紧随其后的重复标题。"""
    out: list[str] = []
    last_heading: str | None = None
    has_content = False
    for line in md.splitlines():
        stripped = line.strip()
        if stripped.startswith("#"):
            text = stripped.lstrip("#").strip()
            if text == last_heading and not has_content:
                continue
            last_heading = text
            has_content = False
            out.append(line)
            continue
        if stripped:
            has_content = True
        out.append(line)
    return re.sub(r"\n{3,}", "\n\n", "\n".join(out))


def fetch_mermaid(route: str) -> list[str]:
    """从 GitHub 原始 Markdown 取 mermaid 图源（SSR HTML 里图为空容器）。"""
    rel = route[len(DOC_ROOT):].strip("/")
    sections = {"guide", "api", "mcp", "platform", "billing", "faq"}
    if not rel or rel in sections:
        raw_path = route.rstrip("/") + "/index.md"
    else:
        raw_path = route + ".md"
    url = RAW_DOCS + urllib.parse.quote(raw_path, safe="/")
    try:
        req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
        with urllib.request.urlopen(req, timeout=20) as resp:
            text = resp.read().decode("utf-8", "replace")
    except Exception:  # noqa: BLE001
        return []
    return [m.group(1).strip() for m in
            re.finditer(r"```mermaid\s*\n(.*?)```", text, re.S)]


# --------------------------------------------------------------------------- #
# 主流程
# --------------------------------------------------------------------------- #
def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--check", action="store_true", help="只输出差异，不写文件")
    ap.add_argument("--only", default="", help="只同步指定章节，如 api,guide")
    args = ap.parse_args()

    print("发现文档目录 …")
    index_html = fetch(DOC_ROOT)
    routes = discover_routes(index_html)
    if args.only:
        prefixes = tuple(DOC_ROOT + p.strip().strip("/") + "/" for p in args.only.split(","))
        routes = [r for r in routes if r.startswith(prefixes)]
    print(f"共 {len(routes)} 个页面")

    route_map = {r: out_path_for(r) for r in routes}
    # 站点内的索引页链接可能写成 /api/index，补个别名
    route_map.update({r + "/index": p for r, p in list(route_map.items())
                      if p.stem == "index"})
    manifest: list[dict] = []
    endpoints: list[dict] = []
    changed = 0

    for i, route in enumerate(routes, 1):
        out = route_map[route]
        conv = Converter(route, out.parent, route_map, fetch_mermaid(route))
        try:
            html = fetch(route)
        except Exception as exc:  # noqa: BLE001
            print(f"  ! {route} 抓取失败：{exc}")
            continue
        title, md = conv.page(html)
        if title in ("", "文档中心"):
            title = SECTION_TITLES.get(route, route.rsplit("/", 1)[-1])
        if not md:
            print(f"  ! {route} 正文为空，跳过")
            continue
        slug = route[len(DOC_ROOT):].strip("/") or "index"
        header = (f"<!-- 来源：{SITE}{urllib.parse.quote(route, safe='/')} "
                  f"| 由 sync-docs.py 自动生成，请勿手改 -->\n\n")
        content = header + f"# {title}\n\n" + md if title else header + md
        endpoints.extend(conv.endpoints)
        old = out.read_text(encoding="utf-8") if out.exists() else None
        if old != content:
            changed += 1
            if not args.check:
                out.parent.mkdir(parents=True, exist_ok=True)
                out.write_text(content, encoding="utf-8")
            print(f"  {'~' if old is not None else '+'} {slug}")
        manifest.append({
            "route": route,
            "title": title,
            "file": str(out.relative_to(SKILL_DIR)),
            "url": SITE + urllib.parse.quote(route, safe="/"),
        })

    if args.check:
        print(f"检查完成：{changed} 个页面有更新")
        return 0

    # 端点速查表与文档总目录
    write_endpoint_index(endpoints)
    write_doc_index(manifest)

    manifest.sort(key=lambda m: m["route"])
    MANIFEST.write_text(
        json.dumps({"source": SITE + DOC_ROOT, "syncedAt": time.strftime("%Y-%m-%d"),
                    "pages": manifest}, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    # 清理已下线的页面
    known = {SKILL_DIR / m["file"] for m in manifest}
    for path in REF_DIR.rglob("*.md"):
        if path.name in ("INDEX.md", "api-quick-reference.md"):
            continue
        if path not in known:
            path.unlink()
            print(f"  - 移除下线页面 {path.relative_to(SKILL_DIR)}")

    print(f"完成：{len(manifest)} 个页面，{changed} 个有更新")
    return 0


def write_doc_index(manifest: list[dict]) -> None:
    groups: dict[str, list[dict]] = {}
    for page in manifest:
        rel = Path(page["file"]).relative_to("references")
        section = rel.parts[0] if len(rel.parts) > 1 else "总览"
        groups.setdefault(section, []).append(page)
    lines = [
        "<!-- 由 sync-docs.py 自动生成，请勿手改 -->",
        "",
        "# 文档目录",
        "",
        f"共 {len(manifest)} 篇，镜像自 {SITE + DOC_ROOT}。"
        "接口端点速查见 [api-quick-reference.md](api-quick-reference.md)。",
        "",
    ]
    for section in sorted(groups):
        lines += [f"## {section}", "", "| 文档 | 本地文件 | 原文 |", "| --- | --- | --- |"]
        for page in sorted(groups[section], key=lambda p: p["route"]):
            name = page["title"] or page["route"]
            rel = os.path.relpath(SKILL_DIR / page["file"], REF_DIR).replace(os.sep, "/")
            lines.append(f"| {name} | [{rel}]({rel}) | {page['url']} |")
        lines.append("")
    (REF_DIR / "INDEX.md").write_text("\n".join(lines), encoding="utf-8")


def write_endpoint_index(endpoints: list[dict]) -> None:
    if not endpoints:
        return
    seen = set()
    rows = []
    for ep in endpoints:
        key = (ep["method"], ep["url"])
        if key in seen:
            continue
        seen.add(key)
        ep_route = ep["route"]
        target = out_path_for(ep_route)
        rel = os.path.relpath(target, REF_DIR).replace(os.sep, "/")
        rows.append(f"| `{ep['method']}` | `{ep['url']}` | {ep['title']} | [{target.stem}]({rel}) |")
    body = "\n".join([
        "<!-- 由 sync-docs.py 自动生成，请勿手改 -->",
        "",
        "# 平台 API 速查",
        "",
        f"共 {len(rows)} 个端点。Base URL `https://xrobo.qiniu.com`，"
        "认证头 `Authorization: Bearer <token|api_key>`。",
        "",
        "| 方法 | 路径 | 说明 | 详情 |",
        "| --- | --- | --- | --- |",
        *rows,
        "",
    ])
    path = REF_DIR / "api-quick-reference.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")


if __name__ == "__main__":
    raise SystemExit(main())
