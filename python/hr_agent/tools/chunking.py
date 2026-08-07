from __future__ import annotations


def chunk_text(text: str, size: int = 400, overlap: int = 60) -> list[str]:
    """Split long bank items; short items stay one chunk."""
    text = (text or "").strip()
    if not text:
        return []
    if size <= 0:
        return [text]
    if len(text) <= size:
        return [text]

    # Prefer paragraph / sentence boundaries.
    parts: list[str] = []
    for block in text.replace("\r\n", "\n").split("\n\n"):
        block = block.strip()
        if block:
            parts.append(block)
    if len(parts) <= 1:
        parts = [text]

    chunks: list[str] = []
    buf = ""
    for part in parts:
        if not buf:
            buf = part
            continue
        if len(buf) + 1 + len(part) <= size:
            buf = buf + "\n\n" + part
        else:
            chunks.extend(_window(buf, size, overlap))
            buf = part
    if buf:
        chunks.extend(_window(buf, size, overlap))
    return [c for c in chunks if c.strip()]


def _window(text: str, size: int, overlap: int) -> list[str]:
    if len(text) <= size:
        return [text]
    step = max(1, size - max(0, overlap))
    out: list[str] = []
    i = 0
    while i < len(text):
        out.append(text[i : i + size].strip())
        if i + size >= len(text):
            break
        i += step
    return [c for c in out if c]
