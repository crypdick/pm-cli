"""Extract readable email evidence without fetching external HTML resources."""

from html.parser import HTMLParser
from typing import override

_BLOCKS = frozenset({"p", "div", "br", "li", "tr", "hr", "h1", "h2", "h3", "blockquote"})
_HIDDEN = frozenset({"head", "script", "style", "template"})
_VOID = frozenset(
    {
        "area",
        "base",
        "br",
        "col",
        "embed",
        "hr",
        "img",
        "input",
        "link",
        "meta",
        "param",
        "source",
        "track",
        "wbr",
    }
)


class _MailText(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.hidden: str | None = None
        self.hidden_depth = 0
        self.links: list[tuple[str, int]] = []

    @override
    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if self.hidden is not None:
            if tag == self.hidden:
                self.hidden_depth += 1
            return
        attributes = dict(attrs)
        styles = {}
        for declaration in (attributes.get("style") or "").casefold().split(";"):
            name, _, value = declaration.partition(":")
            styles[name.strip()] = value.partition("!")[0].strip()
        if (
            tag in _HIDDEN
            or "hidden" in attributes
            or styles.get("display") == "none"
            or styles.get("visibility") == "hidden"
        ):
            if tag not in _VOID:
                self.hidden = tag
                self.hidden_depth = 1
            return
        if tag in _BLOCKS:
            self.parts.append("\n")
        if tag == "td":
            self.parts.append(" ")
        if tag == "img" and (alt := attributes.get("alt")):
            self.parts.append(alt)
        if tag == "a":
            href = attributes.get("href") or ""
            self.links.append((href, len(self.parts)))

    @override
    def handle_endtag(self, tag: str) -> None:
        if self.hidden is not None:
            if tag == self.hidden:
                self.hidden_depth -= 1
                if self.hidden_depth == 0:
                    self.hidden = None
            return
        if tag == "a" and self.links:
            href, start = self.links.pop()
            if href.casefold().startswith(("https://", "http://", "mailto:")) and href not in (
                "".join(self.parts[start:])
            ):
                self.parts.append(f" ({href})")
        if tag in _BLOCKS:
            self.parts.append("\n")

    @override
    def handle_data(self, data: str) -> None:
        if self.hidden is None:
            self.parts.append(data)


def readable_html(content: str) -> str:
    """Preserve visible text and safe link targets without loading resources."""
    parser = _MailText()
    parser.feed(content)
    parser.close()
    lines = (" ".join(line.split()) for line in "".join(parser.parts).splitlines())
    return "\n".join(line for line in lines if line)
