from .core import norm_ws as _norm


def wrap(text: str, width: int) -> list[str]:
    lines, cur = [], ""
    for word in _norm(text).split(" "):
        if cur and len(cur) + 1 + len(word) > width:
            lines.append(cur)
            cur = word
        else:
            cur = f"{cur} {word}" if cur else word
    if cur:
        lines.append(cur)
    return lines
