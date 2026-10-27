import re

_WS = re.compile(r"\s+")


def norm_ws(text: str) -> str:
    """Collapses runs of whitespace into single spaces and trims the ends."""
    return _WS.sub(" ", text).strip()


def norm_ws_count(text: str) -> int:
    """How many whitespace runs norm_ws would collapse."""
    return len(_WS.findall(text.strip()))
