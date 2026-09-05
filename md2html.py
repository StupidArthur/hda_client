# -*- coding: utf-8 -*-
"""最小的 Markdown -> HTML 转换（够用即停），输出带样式的独立 HTML。"""

import re
import sys
from pathlib import Path

HTML_TEMPLATE = """<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
  body {{ font-family: "Microsoft YaHei", "PingFang SC", sans-serif; max-width: 820px;
         margin: 32px auto; padding: 0 24px; color: #222; font-size: 14px; line-height: 1.7; }}
  h1 {{ font-size: 22px; border-bottom: 2px solid #2b6cb0; padding-bottom: 8px; }}
  h2 {{ font-size: 18px; border-bottom: 1px solid #d0d7de; padding-bottom: 6px; margin-top: 28px; }}
  h3 {{ font-size: 15px; margin-top: 22px; }}
  code {{ background: #f0f2f5; padding: 2px 5px; border-radius: 3px;
          font-family: Consolas, monospace; font-size: 13px; }}
  pre {{ background: #f6f8fa; padding: 12px; border-radius: 6px; overflow-x: auto; }}
  pre code {{ background: none; padding: 0; }}
  table {{ border-collapse: collapse; width: 100%; margin: 12px 0; }}
  th, td {{ border: 1px solid #d0d7de; padding: 7px 10px; text-align: left; font-size: 13px; }}
  th {{ background: #f0f2f5; }}
  blockquote {{ border-left: 4px solid #d0d7de; margin: 12px 0; padding: 2px 14px; color: #555; }}
  li {{ margin: 3px 0; }}
</style></head><body>
{body}
</body></html>
"""


def escape(s: str) -> str:
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def inline(s: str) -> str:
    s = escape(s)
    s = re.sub(r"`([^`]+)`", r"<code>\1</code>", s)
    s = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", s)
    return s


def convert(md: str) -> str:
    lines = md.splitlines()
    out: list[str] = []
    i = 0
    n = len(lines)

    def flush_block() -> None:
        pass

    while i < n:
        line = lines[i]

        if line.startswith("```"):
            buf = []
            i += 1
            while i < n and not lines[i].startswith("```"):
                buf.append(lines[i])
                i += 1
            i += 1
            out.append("<pre><code>" + escape("\n".join(buf)) + "</code></pre>")
            continue

        if line.startswith("```"):
            i += 1
            continue

        if re.match(r"^#{1,6} ", line):
            level = len(line) - len(line.lstrip("#"))
            text = inline(line[level + 1 :].strip())
            out.append(f"<h{level}>{text}</h{level}>")
            i += 1
            continue

        if line.strip() == "---":
            out.append("<hr>")
            i += 1
            continue

        if line.startswith(">"):
            buf = []
            while i < n and lines[i].startswith(">"):
                buf.append(inline(lines[i][1:].strip()))
                i += 1
            out.append("<blockquote>" + "<br>".join(buf) + "</blockquote>")
            continue

        if line.startswith(("|", " ", "| ")) and "|" in line:
            rows = []
            while i < n and "|" in lines[i]:
                cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
                rows.append(cells)
                i += 1
            if len(rows) >= 2 and all(re.match(r"^:?-+:?$", c) for c in rows[1]):
                header, rows = rows[0], rows[2:]
                head = "".join(f"<th>{inline(c)}</th>" for c in header)
                body = "".join(
                    "<tr>" + "".join(f"<td>{inline(c)}</td>" for c in r) + "</tr>"
                    for r in rows
                )
                out.append(f"<table><thead><tr>{head}</tr></thead><tbody>{body}</tbody></table>")
            continue

        if line.strip().startswith(("- ", "* ")):
            items = []
            while i < n and re.match(r"^\s*[-*] ", lines[i]):
                items.append("<li>" + inline(lines[i].strip()[2:]) + "</li>")
                i += 1
            out.append("<ul>" + "".join(items) + "</ul>")
            continue

        if re.match(r"^\s*\d+\. ", line):
            items = []
            while i < n and re.match(r"^\s*\d+\. ", lines[i]):
                items.append("<li>" + inline(re.sub(r"^\s*\d+\. ", "", lines[i])) + "</li>")
                i += 1
            out.append("<ol>" + "".join(items) + "</ol>")
            continue

        if line.strip() == "":
            i += 1
            continue

        buf = []
        while i < n and lines[i].strip() != "" and not re.match(r"^(#{1,6} |```|>|[-*] |\d+\. )", lines[i]):
            buf.append(lines[i])
            i += 1
        out.append("<p>" + inline(" ".join(x.strip() for x in buf)) + "</p>")

    return "\n".join(out)


def main() -> None:
    for path in sys.argv[1:]:
        p = Path(path)
        md = p.read_text(encoding="utf-8")
        html = HTML_TEMPLATE.format(body=convert(md))
        out = p.with_suffix(".html")
        out.write_text(html, encoding="utf-8")
        print(out)


if __name__ == "__main__":
    main()
