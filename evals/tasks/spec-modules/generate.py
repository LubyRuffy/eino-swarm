"""Writes durations/, semver/ and ranges/ into the workspace given as argv[1].

Each directory holds a README.md of numbered rules and a test_basic.py that
covers only the plain cases. The module itself is for the agent to write. The
tests that check every rule are kept beside the task, in hidden/, so the agent
cannot read them. Pass --fixed to also write the reference solutions (the
harness tests use this to prove the hidden tests are satisfiable).
Deterministic.
"""

import sys
from pathlib import Path

MODULES = {}

MODULES["durations"] = {
    "readme": """# durations

Write `durations.py` with `parse_duration(text) -> int` (seconds) and
`format_duration(seconds) -> str`.

## parse_duration

1. A duration is one or more `<integer><unit>` parts, for example `1h30m`,
   `90s`, `2d4h`. Units are `d`=86400, `h`=3600, `m`=60, `s`=1.
2. Units are case-insensitive: `1H30M` is valid.
3. Whitespace around the whole text and between parts is allowed:
   ` 1h 30m ` is 5400. Whitespace inside a part (`1 h`) is an error.
4. Parts go from the largest unit to the smallest (`30m1h` is an error) and
   each unit appears at most once (`1h1h` is an error).
5. `0s` and `0m` are valid and give 0. Integers carry no sign: `-5s` and
   `+5s` are errors.
6. Empty or whitespace-only text, text without a unit (`90`) and an unknown
   unit (`5w`) raise `ValueError`.
7. Anything that is not a `str` raises `TypeError`.

## format_duration

8. Largest unit first, zero parts omitted: 5400 is `1h30m`, 86400 is `1d`,
   90061 is `1d1h1m1s`.
9. 0 formats as `0s`. A negative number or anything that is not an `int`
   raises `ValueError`; a `bool` is not an `int` here.
""",
    "basic": '''import unittest

from durations import format_duration, parse_duration


class BasicTest(unittest.TestCase):
    def test_parses_a_few_durations(self):
        self.assertEqual(parse_duration("1h30m"), 5400)
        self.assertEqual(parse_duration("90s"), 90)
        self.assertEqual(parse_duration("2d4h"), 187200)

    def test_formats_a_few_durations(self):
        self.assertEqual(format_duration(5400), "1h30m")
        self.assertEqual(format_duration(86400), "1d")


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import re

_PART = re.compile(r"([0-9]+)([A-Za-z])\\s*")
_SIZE = {"d": 86400, "h": 3600, "m": 60, "s": 1}
_ORDER = {"d": 0, "h": 1, "m": 2, "s": 3}


def parse_duration(text):
    if not isinstance(text, str):
        raise TypeError("text must be a str")
    s = text.strip()
    if not s:
        raise ValueError("empty duration")
    pos, total, last = 0, 0, -1
    while pos < len(s):
        m = _PART.match(s, pos)
        if not m:
            raise ValueError("bad duration %r" % text)
        unit = m.group(2).lower()
        if unit not in _SIZE:
            raise ValueError("unknown unit %r" % unit)
        if _ORDER[unit] <= last:
            raise ValueError("units out of order or repeated in %r" % text)
        last = _ORDER[unit]
        total += int(m.group(1)) * _SIZE[unit]
        pos = m.end()
    return total


def format_duration(seconds):
    if isinstance(seconds, bool) or not isinstance(seconds, int):
        raise ValueError("seconds must be an int")
    if seconds < 0:
        raise ValueError("seconds must not be negative")
    if seconds == 0:
        return "0s"
    out = []
    for unit in "dhms":
        n, seconds = divmod(seconds, _SIZE[unit])
        if n:
            out.append("%d%s" % (n, unit))
    return "".join(out)
''',
}

MODULES["semver"] = {
    "readme": """# semver

Write `semver.py` with `parse(text)`, `compare(a, b)` and
`sort_versions(versions)`.

1. `parse(text)` returns `(major, minor, patch, prerelease)` where the first
   three are ints and `prerelease` is a tuple of identifiers: ints for
   all-digit identifiers, strings otherwise. `parse("1.2.3-alpha.1")` is
   `(1, 2, 3, ("alpha", 1))`; with no prerelease it is `()`.
2. An optional leading lowercase `v` is accepted (`v1.2.3`). Surrounding
   whitespace is ignored; whitespace anywhere else is an error.
3. Build metadata after `+` is accepted and dropped: `1.2.3+build5` parses
   like `1.2.3`.
4. There are exactly three numeric parts: `1.2` and `1.2.3.4` raise
   `ValueError`. Numeric parts and numeric prerelease identifiers have no
   leading zeros (`01.2.3` and `1.0.0-01` are errors); `0` itself is fine and
   an identifier such as `0a` is not numeric, so it is allowed. Empty
   identifiers (`1.0.0-` or `1.0.0-a..b`) are errors.
5. `compare(a, b)` returns -1, 0 or 1 comparing major, minor, patch
   numerically (`1.10.0` is greater than `1.9.0`).
6. A version with a prerelease is lower than the same core without one:
   `1.0.0-rc.1` < `1.0.0`.
7. Prerelease identifiers compare left to right. Numeric ones compare as
   numbers (`alpha.2` < `alpha.10`); others compare in ASCII order (`Alpha` <
   `alpha`); a numeric identifier is lower than a non-numeric one (`1` <
   `alpha`); when all shared identifiers are equal the longer prerelease is
   greater (`alpha` < `alpha.1`).
8. Build metadata never affects `compare`: `1.0.0+a` and `1.0.0+b` are 0.
9. `sort_versions(versions)` returns the original strings in ascending order;
   equal versions keep their input order. Every item is validated, so any
   invalid string raises `ValueError` even in a one-element list. Anything
   that is not a `str` raises `TypeError` from `parse`.
""",
    "basic": '''import unittest

from semver import compare, parse, sort_versions


class BasicTest(unittest.TestCase):
    def test_parse_plain_version(self):
        self.assertEqual(parse("1.2.3"), (1, 2, 3, ()))

    def test_compare_numerically(self):
        self.assertEqual(compare("1.10.0", "1.9.0"), 1)
        self.assertEqual(compare("1.0.0", "1.0.0"), 0)

    def test_sort_a_few(self):
        self.assertEqual(sort_versions(["1.10.0", "1.2.0"]), ["1.2.0", "1.10.0"])


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import functools
import re

_VERSION = re.compile(
    r"v?(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z.-]+))?(?:\\+([0-9A-Za-z.-]+))?"
)


def parse(text):
    if not isinstance(text, str):
        raise TypeError("version must be a str")
    m = _VERSION.fullmatch(text.strip())
    if not m:
        raise ValueError("bad version %r" % text)
    pre = []
    if m.group(4) is not None:
        for ident in m.group(4).split("."):
            if ident == "":
                raise ValueError("empty prerelease identifier in %r" % text)
            if ident.isdigit():
                if len(ident) > 1 and ident[0] == "0":
                    raise ValueError("leading zero in %r" % text)
                pre.append(int(ident))
            else:
                pre.append(ident)
    return (int(m.group(1)), int(m.group(2)), int(m.group(3)), tuple(pre))


def _ident(a, b):
    if isinstance(a, int) and isinstance(b, int):
        return (a > b) - (a < b)
    if isinstance(a, int):
        return -1
    if isinstance(b, int):
        return 1
    return (a > b) - (a < b)


def compare(a, b):
    pa, pb = parse(a), parse(b)
    for x, y in zip(pa[:3], pb[:3]):
        if x != y:
            return -1 if x < y else 1
    ra, rb = pa[3], pb[3]
    if ra == rb:
        return 0
    if not ra:
        return 1
    if not rb:
        return -1
    for x, y in zip(ra, rb):
        c = _ident(x, y)
        if c:
            return c
    return (len(ra) > len(rb)) - (len(ra) < len(rb))


def sort_versions(versions):
    items = list(versions)
    for v in items:
        parse(v)
    return sorted(items, key=functools.cmp_to_key(compare))
''',
}

MODULES["ranges"] = {
    "readme": """# ranges

Write `ranges.py` with `parse_ranges(text, max_page) -> list[int]` and
`format_ranges(pages) -> str`.

## parse_ranges

1. `text` is comma-separated items; each is a page `N` or an inclusive range
   `N-M`. `1-3,5` is `[1, 2, 3, 5]`.
2. Whitespace around items and around the `-` is allowed: ` 1 - 3 , 5 `.
3. The result is sorted ascending without duplicates: `5,1-3,2` is
   `[1, 2, 3, 5]`.
4. A range may be open: `7-` is page 7 through `max_page`, `-3` is page 1
   through 3. A lone `-` is an error.
5. Pages run from 1 to `max_page`. Anything outside raises `ValueError`, even
   as part of a range: `1-99` with `max_page=10` is an error, not clipped.
6. `0`, a reversed range (`5-3`), an empty item (`1,,2` or a trailing comma)
   and anything that is not digits, `-` and whitespace raise `ValueError`.
   Empty or whitespace-only `text` is `[]`. `3-3` is `[3]`.
7. A `text` that is not a `str` raises `TypeError`. `max_page` must be an
   `int` of at least 1, otherwise `ValueError`; a `bool` is not an `int`
   here.

## format_ranges

8. `pages` is an iterable of ints of at least 1 (a `bool` or anything else
   raises `ValueError`). The result lists the sorted, de-duplicated pages
   separated by commas, writing a run of three or more consecutive pages as
   `a-b`: `[1, 2, 3, 5, 7, 8, 9]` is `1-3,5,7-9`.
9. A run of exactly two stays two items (`[1, 2, 5]` is `1,2,5`). An empty
   input gives `""`.
""",
    "basic": '''import unittest

from ranges import format_ranges, parse_ranges


class BasicTest(unittest.TestCase):
    def test_parses_a_range_and_a_page(self):
        self.assertEqual(parse_ranges("1-3,5", 10), [1, 2, 3, 5])

    def test_formats_a_run(self):
        self.assertEqual(format_ranges([1, 2, 3, 5]), "1-3,5")


if __name__ == "__main__":
    unittest.main()
''',
    "reference": '''import re


def _page(text, max_page):
    if not re.fullmatch(r"[0-9]+", text):
        raise ValueError("bad page %r" % text)
    n = int(text)
    if n < 1 or n > max_page:
        raise ValueError("page %d is outside 1..%d" % (n, max_page))
    return n


def parse_ranges(text, max_page):
    if not isinstance(text, str):
        raise TypeError("text must be a str")
    if isinstance(max_page, bool) or not isinstance(max_page, int) or max_page < 1:
        raise ValueError("max_page must be an int of at least 1")
    if not text.strip():
        return []
    pages = set()
    for item in text.split(","):
        item = item.strip()
        if not item:
            raise ValueError("empty item")
        if "-" in item:
            lo, _, hi = item.partition("-")
            lo, hi = lo.strip(), hi.strip()
            if not lo and not hi:
                raise ValueError("lone dash")
            a = _page(lo, max_page) if lo else 1
            b = _page(hi, max_page) if hi else max_page
            if a > b:
                raise ValueError("reversed range %r" % item)
            pages.update(range(a, b + 1))
        else:
            pages.add(_page(item, max_page))
    return sorted(pages)


def format_ranges(pages):
    nums = []
    for p in pages:
        if isinstance(p, bool) or not isinstance(p, int) or p < 1:
            raise ValueError("bad page %r" % (p,))
        nums.append(p)
    nums = sorted(set(nums))
    out, i = [], 0
    while i < len(nums):
        j = i
        while j + 1 < len(nums) and nums[j + 1] == nums[j] + 1:
            j += 1
        if j - i >= 2:
            out.append("%d-%d" % (nums[i], nums[j]))
        else:
            out.extend(str(n) for n in nums[i : j + 1])
        i = j + 1
    return ",".join(out)
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
