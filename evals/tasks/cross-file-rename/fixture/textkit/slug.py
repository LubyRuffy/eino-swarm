import re

from .core import norm_ws


def slugify(text: str) -> str:
    text = norm_ws(text).lower()
    return re.sub(r"[^a-z0-9]+", "-", text).strip("-")
