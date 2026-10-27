from . import core

SMALL = {"a", "an", "and", "of", "the", "to"}


def title_case(text: str) -> str:
    words = core.norm_ws(text).split(" ")
    out = []
    for i, w in enumerate(words):
        out.append(w if i and w.lower() in SMALL else w[:1].upper() + w[1:])
    return " ".join(out)
