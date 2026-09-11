"""Unified-diff parsing helpers.

The key job is mapping each *added* line to its line number in the new file,
because GitHub inline review comments must anchor to new-file line numbers.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

# Matches a hunk header like: @@ -12,3 +14,9 @@ optional section heading
_HUNK_RE = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@")


@dataclass
class AddedLine:
    line_no: int  # line number in the NEW file
    text: str     # line content without the leading '+'


def added_lines(patch: str) -> list[AddedLine]:
    """Walk a unified diff hunk and return its added lines with new-file
    line numbers. Removed lines do not advance the new-file counter; context
    and added lines do."""
    out: list[AddedLine] = []
    new_line = 0
    for raw in patch.splitlines():
        m = _HUNK_RE.match(raw)
        if m:
            new_line = int(m.group(1))
            continue
        if raw.startswith("+++") or raw.startswith("---"):
            continue
        if raw.startswith("+"):
            out.append(AddedLine(line_no=new_line, text=raw[1:]))
            new_line += 1
        elif raw.startswith("-"):
            continue
        else:  # context line
            new_line += 1
    return out


def chunk_text(text: str, size: int = 40, overlap: int = 10) -> list[str]:
    """Split text into overlapping line-windows for embedding."""
    lines = text.splitlines()
    if not lines:
        return []
    chunks: list[str] = []
    step = max(1, size - overlap)
    for start in range(0, len(lines), step):
        window = "\n".join(lines[start:start + size])
        if window.strip():
            chunks.append(window)
        if start + size >= len(lines):
            break
    return chunks
