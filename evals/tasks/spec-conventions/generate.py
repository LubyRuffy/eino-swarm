"""The spec-edges workspace plus one repository-wide CONVENTIONS.md that each
module README points to in a single line. The conventions are cross-cutting
(exception messages, exported names, docstrings, keyword-only options,
suppressed exception context) and carry no worked examples, which is where an
author's single pass is most likely to skip a rule a reviewer reading the
rules one by one would catch. Pass --fixed to also write reference solutions.
Deterministic.
"""

import importlib.util
import sys
from pathlib import Path

_spec = importlib.util.spec_from_file_location(
    "spec_edges_generate",
    Path(__file__).resolve().parent.parent / "spec-edges" / "generate.py",
)
base = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(base)

CONVENTIONS = """# Conventions

Every module in this repository follows these rules in addition to its own
README.

1. A module defines `__all__` listing exactly its public functions.
2. Every public function has a docstring.
3. Optional parameters are keyword-only: callers pass them by name.
4. Every exception a public function raises because of its input has a
   message that starts with that function's name and a colon.
5. An exception raised while handling another one hides it (`from None`):
   callers never see an internal error as its context.
"""

POINTER = "\nAlso follow the repository-wide rules in `../CONVENTIONS.md`.\n"

REFERENCE = {
    "money": '''"""Amounts in cents."""

import re
from decimal import ROUND_HALF_UP, Decimal, localcontext

__all__ = ["parse_amount", "format_amount"]

_NUMBER = re.compile(r"([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(?:\\.([0-9]{1,3}))?")


def parse_amount(text):
    """Parse an amount into cents."""
    if not isinstance(text, str):
        raise TypeError("parse_amount: text must be a str")
    s = text.strip()
    if not s:
        raise ValueError("parse_amount: empty amount")
    negative = False
    if s.startswith("(") and s.endswith(")"):
        negative, s = True, s[1:-1]
    elif s.startswith("-"):
        negative, s = True, s[1:]
    m = _NUMBER.fullmatch(s)
    if not m:
        raise ValueError("parse_amount: bad amount %r" % text)
    whole = m.group(1).replace(",", "")
    with localcontext() as ctx:
        ctx.prec = len(whole) + 8
        cents = (Decimal(whole + "." + (m.group(2) or "0")) * 100).quantize(Decimal(1), rounding=ROUND_HALF_UP)
    return -int(cents) if negative else int(cents)


def format_amount(cents):
    """Format cents as an amount."""
    if isinstance(cents, bool) or not isinstance(cents, int):
        raise TypeError("format_amount: cents must be an int")
    whole, part = divmod(abs(cents), 100)
    groups = []
    while whole >= 1000:
        whole, rest = divmod(whole, 1000)
        groups.append("%03d" % rest)
    groups.append(str(whole))
    digits = "".join(reversed(groups))
    if len(digits) >= 5:
        digits = ",".join(reversed(groups))
    out = "%s.%02d" % (digits, part)
    return "(%s)" % out if cents < 0 else out
''',
    "slug": '''"""URL slugs."""

import re
import unicodedata

__all__ = ["slugify"]

_LETTERS = {"ß": "ss", "æ": "ae", "ø": "o", "ł": "l"}


def slugify(text, *, max_len=32):
    """Turn text into a slug."""
    if not isinstance(text, str):
        raise TypeError("slugify: text must be a str")
    if isinstance(max_len, bool) or not isinstance(max_len, int):
        raise TypeError("slugify: max_len must be an int")
    if max_len < 8:
        raise ValueError("slugify: max_len must be at least 8")
    s = text.lower().replace("'", "").replace("\\u2019", "").replace("&", " and ")
    s = "".join(_LETTERS.get(c, c) for c in s)
    s = "".join(c for c in unicodedata.normalize("NFKD", s) if not unicodedata.combining(c))
    s = re.sub(r"[^a-z0-9]+", "-", s).strip("-")
    if s[:1].isdigit():
        s = "n-" + s
    if len(s) > max_len:
        cut = s[: max_len + 1].rfind("-")
        s = s[:cut] if cut > 0 else s[:max_len]
    return s or "untitled"
''',
}


def main(out, fixed=False):
    out = Path(out)
    base.main(out, fixed=fixed)
    (out / "CONVENTIONS.md").write_text(CONVENTIONS)
    for name in base.MODULES:
        readme = out / name / "README.md"
        readme.write_text(readme.read_text() + POINTER)
    if fixed:
        for name, source in REFERENCE.items():
            (out / name / (name + ".py")).write_text(source)
        headers = out / "headers" / "headers.py"
        src = base.MODULES["headers"]["reference"]
        src = '"""CSV header names."""\n\n' + src.replace(
            "\n\ndef _base", '\n__all__ = ["normalize_headers"]\n\n\ndef _base', 1)
        src = src.replace("def normalize_headers(names):\n",
                          'def normalize_headers(names):\n    """Unique column names for a header row."""\n', 1)
        src = src.replace('"names must be a list or tuple"', '"normalize_headers: names must be a list or tuple"')
        src = src.replace('"every name must be a str"', '"normalize_headers: every name must be a str"')
        headers.write_text(src)


if __name__ == "__main__":
    main(sys.argv[1], fixed="--fixed" in sys.argv[2:])
