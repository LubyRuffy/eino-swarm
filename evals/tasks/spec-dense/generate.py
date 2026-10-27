"""Writes duration/ into the workspace given as argv[1].

The spec-edges shape with one module whose thirteen rules interact: unit
order, a zero exception, a fraction allowed only on the last component and
only when it lands on whole seconds, a sign that applies to the whole text,
a second (ISO 8601) syntax that inherits the number rules, and digits that
Python's `\\d` and `int()` accept but the README does not. Neither model
missed a spec-edges or spec-conventions rule unreviewed; this fixture asks
whether rule density, rather than any single surprising rule, leaves
something for a review to catch. Pass --fixed to also write the reference
solution. Deterministic.
"""

import sys
from pathlib import Path

README = """# duration

Write `duration.py` with `parse_duration(text) -> int` (seconds) and
`format_duration(seconds) -> str`.

## parse_duration

1. The short form is one or more components, each a number followed by a
   unit: `d` (86400 seconds), `h` (3600), `m` (60) or `s` (1). `1h30m` is
   5400, `2d` is 172800, `45s` is 45. Units are lowercase only: `1H` raises
   `ValueError`.
2. Components appear in the order d, h, m, s, each at most once, and any of
   them may be skipped: `1d5s` is 86405; `30m1h` and `1h1h` raise
   `ValueError`.
3. A number is written with the ASCII digits `0`-`9` only. Underscores and
   any other digit characters (for example `１`, FULLWIDTH DIGIT ONE, or
   `٣`, ARABIC-INDIC DIGIT THREE) raise `ValueError`. A number has no leading
   zeros: `01h` and `00s` raise `ValueError`.
4. A component whose number is zero is an error (`0h30m`, `1h0s`, `0.0m`),
   with one exception: the whole text `0s` means zero.
5. Only the last component may have a fraction, written as `.` and one to
   three digits: `1.5h` is 5400, `0.5h` is 1800, `1h1.5m` is 3690.
   `1.5h30m`, `1.h`, `.5h` and `1.2345s` raise `ValueError`. The fraction
   must come to whole seconds: `1.5m` is 90, but `0.5s` and `1.001m` raise
   `ValueError`.
6. A single leading `-` negates the whole duration: `-1h30m` is -5400.
   `+1h`, `--1h`, `1h-30m` and `-0s` raise `ValueError`.
7. The ISO 8601 form is also accepted: `P`, then an optional `<n>D`, then
   optionally `T` followed by one or more of `<n>H`, `<n>M`, `<n>S` in that
   order. Letters are uppercase only. `PT1H30M` is 5400, `P2D` is 172800,
   `P1DT5S` is 86405. `P`, `PT`, `P1DT`, `PT1h` and `pt1h` raise
   `ValueError`. Rules 3 to 6 apply to the ISO form too, with `PT0S` as its
   zero: `-PT1H` is -3600, `PT1.5H` is 5400, and `PT0H`, `P1DT1.5H30M` and
   `-PT0S` raise `ValueError`.
8. No whitespace anywhere, including at either end: ` 1h`, `1h ` and
   `1h 30m` raise `ValueError`. Empty text raises `ValueError`.
9. A result whose magnitude exceeds ten years of 365 days (315360000
   seconds) raises `OverflowError`; exactly that many is allowed.
10. Anything that is not a `str` raises `TypeError`.

## format_duration

11. The short form, largest unit first, zero components left out: 5400 is
    `1h30m`, 86405 is `1d5s`, 0 is `0s`, -90 is `-1m30s`.
12. Anything that is not an `int` raises `TypeError`; a `bool` is not an
    `int` here. A value beyond rule 9's limit raises `OverflowError`.
13. For every value `format_duration` accepts,
    `parse_duration(format_duration(n)) == n`.
"""

BASIC = '''import unittest

from duration import format_duration, parse_duration


class BasicTest(unittest.TestCase):
    def test_parses_plain_durations(self):
        self.assertEqual(parse_duration("1h30m"), 5400)
        self.assertEqual(parse_duration("45s"), 45)

    def test_formats_plain_durations(self):
        self.assertEqual(format_duration(5400), "1h30m")
        self.assertEqual(format_duration(45), "45s")


if __name__ == "__main__":
    unittest.main()
'''

REFERENCE = '''import re

LIMIT = 315360000
_SECONDS = {"d": 86400, "h": 3600, "m": 60, "s": 1, "D": 86400, "H": 3600, "M": 60, "S": 1}
_ORDER = "dhmsDHMS"


def _components(body, units):
    pattern = re.compile(r"(0|[1-9][0-9]*)(?:\\.([0-9]{1,3}))?([%s])" % units)
    pos, out = 0, []
    while pos < len(body):
        m = pattern.match(body, pos)
        if not m:
            raise ValueError("bad component at %r" % body[pos:])
        out.append(m.groups())
        pos = m.end()
    return out


def parse_duration(text):
    if not isinstance(text, str):
        raise TypeError("text must be a str")
    if text in ("0s", "PT0S"):
        return 0
    negative = text.startswith("-")
    body = text[1:] if negative else text
    if body.startswith("P"):
        date, sep, time = body[1:].partition("T")
        parts = _components(date, "D") + _components(time, "HMS")
        if (sep and not time) or not parts:
            raise ValueError("incomplete ISO duration")
    else:
        parts = _components(body, "dhms")
        if not parts:
            raise ValueError("empty duration")
    total, last, huge = 0, -1, False
    for i, (whole, frac, unit) in enumerate(parts):
        rank = _ORDER.index(unit) % 4
        if rank <= last:
            raise ValueError("components out of order")
        last = rank
        if frac is not None and i != len(parts) - 1:
            raise ValueError("only the last component may have a fraction")
        if len(whole) > 12:
            huge = True
            continue
        millis = (int(whole) * 1000 + int((frac or "").ljust(3, "0"))) * _SECONDS[unit]
        if millis == 0:
            raise ValueError("zero component")
        if millis % 1000:
            raise ValueError("not whole seconds")
        total += millis // 1000
    if huge or total > LIMIT:
        raise OverflowError("duration too long")
    return -total if negative else total


def format_duration(seconds):
    if type(seconds) is not int:
        raise TypeError("seconds must be an int")
    if abs(seconds) > LIMIT:
        raise OverflowError("duration too long")
    if seconds == 0:
        return "0s"
    rest, out = abs(seconds), "-" if seconds < 0 else ""
    for unit in "dhms":
        n, rest = divmod(rest, _SECONDS[unit])
        if n:
            out += "%d%s" % (n, unit)
    return out
'''


def main(out, fixed=False):
    d = Path(out) / "duration"
    d.mkdir(parents=True, exist_ok=True)
    (d / "README.md").write_text(README)
    (d / "test_basic.py").write_text(BASIC)
    if fixed:
        (d / "duration.py").write_text(REFERENCE)


if __name__ == "__main__":
    main(sys.argv[1], fixed="--fixed" in sys.argv[2:])
