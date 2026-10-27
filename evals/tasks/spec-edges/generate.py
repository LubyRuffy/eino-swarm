"""Writes money/, slug/ and headers/ into the workspace given as argv[1].

The spec-modules shape (a README of numbered rules, a test_basic.py with only
plain cases, rule tests kept in hidden/) with rules chosen so that the usual
library call or habit gets them wrong: float rounding, thousands separators,
apostrophes, letters NFKD cannot decompose, a prefix that counts toward a
length limit, and name collisions decided by precedence. Every rule is stated
in the README; none is a trick the README hides. Pass --fixed to also write
the reference solutions. Deterministic.
"""

import sys
from pathlib import Path

MODULES = {}

MODULES["money"] = {
    "readme": """# money

Write `money.py` with `parse_amount(text) -> int` (cents) and
`format_amount(cents) -> str`.

## parse_amount

1. An amount is digits with an optional `.` and one, two or three decimals:
   `12` is 1200, `12.5` is 1250, `12.34` is 1234. A third decimal is rounded
   half away from zero: `0.125` is 13, `0.005` is 1, `1.234` is 123. Four or
   more decimals, a bare `.5` and a trailing `5.` raise `ValueError`.
2. The integer part may use `,` thousands separators. When it does, every
   group after the first has exactly three digits and the first has one to
   three: `1,234.5` and `12,345,678` are valid; `12,34`, `1,2345`, `1234,567`
   and `,123` raise `ValueError`. Separators are optional at any size.
3. A negative amount is written either `-12.00` or `(12.00)`, never both:
   `-(12.00)` and `(-12.00)` raise `ValueError`. Rounding is applied to the
   magnitude, so `-0.125` and `(0.125)` are -13. A leading `+` is an error.
4. Whitespace around the text is ignored; whitespace anywhere inside it is an
   error. Empty or whitespace-only text raises `ValueError`.
5. Anything that is not a `str` raises `TypeError`.

## format_amount

6. Always two decimals. Thousands separators appear only when the integer
   part has five or more digits: 123456 is `1234.56`, 1234567 is
   `12,345.67`, 100000000 is `1,000,000.00`.
7. A negative amount is written in parentheses: -150 is `(1.50)` and
   -1234567 is `(12,345.67)`. Zero is `0.00`.
8. Anything that is not an `int` raises `TypeError`; a `bool` is not an
   `int` here.
""",
    "basic": '''import unittest

from money import format_amount, parse_amount


class BasicTest(unittest.TestCase):
    def test_parses_plain_amounts(self):
        self.assertEqual(parse_amount("12.34"), 1234)
        self.assertEqual(parse_amount("7"), 700)

    def test_formats_plain_amounts(self):
        self.assertEqual(format_amount(1234), "12.34")
        self.assertEqual(format_amount(5), "0.05")


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import re
from decimal import ROUND_HALF_UP, Decimal, localcontext

_NUMBER = re.compile(r"([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(?:\\.([0-9]{1,3}))?")


def parse_amount(text):
    if not isinstance(text, str):
        raise TypeError("text must be a str")
    s = text.strip()
    if not s:
        raise ValueError("empty amount")
    negative = False
    if s.startswith("(") and s.endswith(")"):
        negative, s = True, s[1:-1]
    elif s.startswith("-"):
        negative, s = True, s[1:]
    m = _NUMBER.fullmatch(s)
    if not m:
        raise ValueError("bad amount %r" % text)
    whole = m.group(1).replace(",", "")
    with localcontext() as ctx:
        ctx.prec = len(whole) + 8
        cents = (Decimal(whole + "." + (m.group(2) or "0")) * 100).quantize(Decimal(1), rounding=ROUND_HALF_UP)
    return -int(cents) if negative else int(cents)


def format_amount(cents):
    if isinstance(cents, bool) or not isinstance(cents, int):
        raise TypeError("cents must be an int")
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
}

MODULES["slug"] = {
    "readme": """# slug

Write `slug.py` with `slugify(text, max_len=32) -> str`.

1. The text is lowercased. Lowercase ASCII letters and digits are kept;
   every other run of characters becomes one `-`, and the result never
   starts or ends with `-`: `Hello, World!` is `hello-world`.
2. Apostrophes (`'` and `’`) are removed without leaving a separator:
   `Don't Stop` is `dont-stop`.
3. `&` reads as the word `and`: `R&D` is `r-and-d`.
4. Accented Latin letters lose their accents (`Crème Brûlée` is
   `creme-brulee`). These letters, which have no accent to drop, are
   replaced as written: `ß`→`ss`, `æ`→`ae`, `ø`→`o`, `ł`→`l` (and their
   capitals). Any other non-ASCII character is a separator.
5. A result that starts with a digit gets the prefix `n-`: `2024 Plan` is
   `n-2024-plan`. The prefix counts toward `max_len`.
6. A result longer than `max_len` keeps the longest run of whole words, from
   the start, that fits in `max_len`; words are the parts between `-`. When
   even the first word is longer than `max_len`, it is cut to `max_len`.
7. A result with no characters left is `untitled`.
8. `max_len` below 8 raises `ValueError`. Text that is not a `str`, or a
   `max_len` that is not an `int`, raises `TypeError`.
""",
    "basic": '''import unittest

from slug import slugify


class BasicTest(unittest.TestCase):
    def test_plain_titles(self):
        self.assertEqual(slugify("Hello World"), "hello-world")
        self.assertEqual(slugify("release notes v2"), "release-notes-v2")


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import re
import unicodedata

_LETTERS = {"ß": "ss", "æ": "ae", "ø": "o", "ł": "l"}


def slugify(text, max_len=32):
    if not isinstance(text, str):
        raise TypeError("text must be a str")
    if isinstance(max_len, bool) or not isinstance(max_len, int):
        raise TypeError("max_len must be an int")
    if max_len < 8:
        raise ValueError("max_len must be at least 8")
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

MODULES["headers"] = {
    "readme": """# headers

Write `headers.py` with `normalize_headers(names) -> list[str]`, which turns
a CSV header row into unique column names.

1. Each name is stripped and lowercased; every run of characters other than
   ASCII letters and digits becomes one `_`, with no `_` left at either end:
   ` Unit Price ($) ` is `unit_price`.
2. A name that starts with a digit gets the prefix `c_`: `2024 Total` is
   `c_2024_total`.
3. A name with nothing left is `col_<position>`, counting columns from 1:
   `["a", "", "b"]` gives `col_2` for the middle one.
4. Repeated names: the first keeps its name, later ones get `_2`, `_3`, ...
   in input order: `["a", "a", "a"]` is `["a", "a_2", "a_3"]`.
5. A suffixed or generated name never takes a name that another column has
   after rules 1–3, wherever that column is: skip to the next free number.
   `["a", "a", "a_2"]` is `["a", "a_3", "a_2"]`. A `col_<position>` name that
   another column already has is itself suffixed: `["", "col_1"]` is
   `["col_1_2", "col_1"]`.
6. An empty list gives `[]`. Anything other than a `list` or `tuple`, or an
   item that is not a `str`, raises `TypeError`.
""",
    "basic": '''import unittest

from headers import normalize_headers


class BasicTest(unittest.TestCase):
    def test_plain_header_row(self):
        self.assertEqual(normalize_headers(["Name", "Email Address"]), ["name", "email_address"])


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import re


def _base(name):
    s = re.sub(r"[^a-z0-9]+", "_", name.strip().lower()).strip("_")
    if s[:1].isdigit():
        s = "c_" + s
    return s


def normalize_headers(names):
    if not isinstance(names, (list, tuple)):
        raise TypeError("names must be a list or tuple")
    if any(not isinstance(n, str) for n in names):
        raise TypeError("every name must be a str")
    bases = [_base(n) for n in names]
    wanted = [b or "col_%d" % (i + 1) for i, b in enumerate(bases)]
    after = set(wanted)
    taken, out = set(), []
    for i, (b, name) in enumerate(zip(bases, wanted)):
        if name in taken or (not b and name in wanted[:i] + wanted[i + 1:]):
            k = 2
            while "%s_%d" % (name, k) in taken or "%s_%d" % (name, k) in after:
                k += 1
            name = "%s_%d" % (name, k)
        taken.add(name)
        out.append(name)
    return out
''',
}


def main(out, fixed=False):
    out = Path(out)
    for name, spec in MODULES.items():
        d = out / name
        d.mkdir(parents=True, exist_ok=True)
        (d / "README.md").write_text(spec["readme"])
        (d / "test_basic.py").write_text(spec["basic"])
        if fixed:
            (d / (name + ".py")).write_text(spec["reference"])


if __name__ == "__main__":
    main(sys.argv[1], fixed="--fixed" in sys.argv[2:])
