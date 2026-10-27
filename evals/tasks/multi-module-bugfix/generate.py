"""Writes pkgs/<name>/ into the workspace given as argv[1].

Six unrelated small packages, each with a test file that fails because of
two defects in the implementation. The sources below are the working
versions; a defect is a single substitution applied to one of them, so every
package is known to be fixable and each defect is known to be caught. Pass
--fixed to write the working versions (the harness tests use this).
slow_s makes importing each test file take that long, standing in for a slow
fixture or build; the multi-module-bugfix-slow task uses it.
Deterministic.
"""

import sys
from pathlib import Path

# name -> (working module source, [(working snippet, defective snippet)], test source)
PACKAGES = {}

PACKAGES["intervals"] = (
    '''"""Merging closed integer intervals.

merge(intervals) takes (start, end) pairs with start <= end, in any order,
and returns a sorted list of tuples. Intervals that overlap or touch (the next
start is at most the current end + 1) are joined: [(1, 3), (4, 6)] -> [(1, 6)].
"""


def merge(intervals):
    out = []
    for start, end in sorted(intervals):
        if out and start <= out[-1][1] + 1:
            out[-1] = (out[-1][0], max(out[-1][1], end))
        else:
            out.append((start, end))
    return out


def total_length(intervals):
    """Number of integers covered by the union, both ends included."""
    return sum(end - start + 1 for start, end in merge(intervals))
''',
    [
        ("if out and start <= out[-1][1] + 1:", "if out and start < out[-1][1]:"),
        ("out[-1] = (out[-1][0], max(out[-1][1], end))", "out[-1] = (out[-1][0], end)"),
    ],
    '''import unittest

from intervals import merge, total_length


class IntervalsTest(unittest.TestCase):
    def test_touching_intervals_join(self):
        self.assertEqual(merge([(1, 3), (4, 6)]), [(1, 6)])

    def test_unordered_input_is_sorted(self):
        self.assertEqual(merge([(8, 9), (1, 2), (4, 5)]), [(1, 2), (4, 5), (8, 9)])

    def test_contained_interval_does_not_shrink_the_result(self):
        self.assertEqual(merge([(1, 10), (2, 3)]), [(1, 10)])

    def test_total_length_counts_both_ends_once(self):
        self.assertEqual(total_length([(1, 3), (3, 5)]), 5)
        self.assertEqual(total_length([]), 0)


if __name__ == "__main__":
    unittest.main()
''',
)

PACKAGES["ringbuf"] = (
    '''"""Fixed-capacity ring buffer.

push(x) appends; once the buffer is full the oldest item is dropped.
items() returns the contents oldest first. latest(n) returns the n newest
items, newest first, or all of them when fewer are stored.
"""


class Ring:
    def __init__(self, capacity):
        if capacity < 1:
            raise ValueError("capacity must be at least 1")
        self.capacity = capacity
        self._data = []

    def push(self, x):
        self._data.append(x)
        if len(self._data) > self.capacity:
            del self._data[0]

    def items(self):
        return list(self._data)

    def latest(self, n):
        return list(reversed(self._data[-n:])) if n > 0 else []

    def __len__(self):
        return len(self._data)
''',
    [
        ("if len(self._data) > self.capacity:", "if len(self._data) >= self.capacity:"),
        ("return list(reversed(self._data[-n:])) if n > 0 else []", "return list(self._data[-n:]) if n > 0 else []"),
    ],
    '''import unittest

from ringbuf import Ring


class RingTest(unittest.TestCase):
    def test_holds_exactly_its_capacity(self):
        r = Ring(3)
        for x in (1, 2, 3):
            r.push(x)
        self.assertEqual((len(r), r.items()), (3, [1, 2, 3]))

    def test_drops_the_oldest_when_full(self):
        r = Ring(3)
        for x in range(1, 6):
            r.push(x)
        self.assertEqual(r.items(), [3, 4, 5])

    def test_latest_is_newest_first(self):
        r = Ring(5)
        for x in (1, 2, 3, 4):
            r.push(x)
        self.assertEqual(r.latest(2), [4, 3])
        self.assertEqual(r.latest(10), [4, 3, 2, 1])
        self.assertEqual(r.latest(0), [])

    def test_rejects_an_empty_capacity(self):
        with self.assertRaises(ValueError):
            Ring(0)


if __name__ == "__main__":
    unittest.main()
''',
)

PACKAGES["versions"] = (
    '''"""Version strings like 1.10.2 or 2.0.0-rc.1.

compare(a, b) returns -1, 0 or 1. Numeric parts compare as numbers, so 1.10
is newer than 1.9, and every numeric part counts. A version with a
pre-release tag (after '-') is older than the same version without one.
Missing numeric parts count as 0: 1.2 equals 1.2.0.
"""


def _parse(v):
    core, _, pre = v.partition("-")
    nums = [int(p) for p in core.split(".")]
    while len(nums) < 3:
        nums.append(0)
    return nums, pre


def compare(a, b):
    na, pa = _parse(a)
    nb, pb = _parse(b)
    if na != nb:
        return -1 if na < nb else 1
    if pa == pb:
        return 0
    if not pa:
        return 1
    if not pb:
        return -1
    return -1 if pa < pb else 1


def newest(versions):
    best = versions[0]
    for v in versions[1:]:
        if compare(v, best) > 0:
            best = v
    return best
''',
    [
        ("nums = [int(p) for p in core.split(\".\")]", "nums = [int(p) for p in core.split(\".\")[:2]]"),
        ("    if not pa:\n        return 1\n    if not pb:\n        return -1\n",
         "    if not pa:\n        return -1\n    if not pb:\n        return 1\n"),
    ],
    '''import unittest

from versions import compare, newest


class VersionsTest(unittest.TestCase):
    def test_numbers_compare_as_numbers(self):
        self.assertEqual(compare("1.10", "1.9"), 1)

    def test_every_numeric_part_counts(self):
        self.assertEqual(compare("1.2.3", "1.2.10"), -1)

    def test_missing_parts_are_zero(self):
        self.assertEqual(compare("1.2", "1.2.0"), 0)

    def test_a_prerelease_is_older_than_its_release(self):
        self.assertEqual(compare("2.0.0-rc.1", "2.0.0"), -1)
        self.assertEqual(compare("2.0.0", "2.0.0-rc.1"), 1)

    def test_newest_picks_the_highest(self):
        self.assertEqual(newest(["1.9.0", "1.10.0", "1.2.0-beta"]), "1.10.0")


if __name__ == "__main__":
    unittest.main()
''',
)

PACKAGES["workdays"] = (
    '''"""Business-day arithmetic. Saturdays and Sundays are not business days.

count_between(start, end) counts the business days in the half-open range
[start, end): start counts, end does not. When end <= start the count is 0.
add_days(start, n) returns the date n business days after start, n >= 0.
"""

from datetime import timedelta


def is_business_day(d):
    return d.weekday() < 5


def count_between(start, end):
    n = 0
    d = start
    while d < end:
        if is_business_day(d):
            n += 1
        d += timedelta(days=1)
    return n


def add_days(start, n):
    d = start
    while n > 0:
        d += timedelta(days=1)
        if is_business_day(d):
            n -= 1
    return d
''',
    [
        ("return d.weekday() < 5", "return d.weekday() <= 5"),
        ("    while d < end:\n", "    while d <= end:\n"),
    ],
    '''import unittest
from datetime import date

from workdays import add_days, count_between, is_business_day

FRI, SAT, SUN, MON = date(2026, 10, 23), date(2026, 10, 24), date(2026, 10, 25), date(2026, 10, 26)


class WorkdaysTest(unittest.TestCase):
    def test_weekend_is_not_a_business_day(self):
        self.assertEqual([is_business_day(d) for d in (FRI, SAT, SUN, MON)], [True, False, False, True])

    def test_the_end_date_is_excluded(self):
        self.assertEqual(count_between(MON, date(2026, 10, 27)), 1)

    def test_a_weekend_adds_nothing(self):
        self.assertEqual(count_between(FRI, MON), 1)
        self.assertEqual(count_between(MON, date(2026, 10, 30)), 4)

    def test_empty_or_reversed_range_is_zero(self):
        self.assertEqual(count_between(MON, MON), 0)
        self.assertEqual(count_between(MON, FRI), 0)

    def test_adding_days_skips_the_weekend(self):
        self.assertEqual(add_days(FRI, 1), MON)
        self.assertEqual(add_days(MON, 5), date(2026, 11, 2))


if __name__ == "__main__":
    unittest.main()
''',
)

PACKAGES["lrucache"] = (
    '''"""Least-recently-used cache with a fixed capacity.

get(key) returns the value, or None when absent, and marks the key as the
most recently used. put(key, value) stores it, replacing any old value and
marking it most recently used; when that would exceed the capacity, the
least recently used key is evicted. keys() lists keys from least to most
recently used.
"""

from collections import OrderedDict


class LRU:
    def __init__(self, capacity):
        self.capacity = capacity
        self._d = OrderedDict()

    def get(self, key):
        if key not in self._d:
            return None
        self._d.move_to_end(key)
        return self._d[key]

    def put(self, key, value):
        self._d[key] = value
        self._d.move_to_end(key)
        while len(self._d) > self.capacity:
            self._d.popitem(last=False)

    def keys(self):
        return list(self._d)
''',
    [
        ("        self._d.move_to_end(key)\n        return self._d[key]", "        return self._d[key]"),
        ("self._d.popitem(last=False)", "self._d.popitem(last=True)"),
    ],
    '''import unittest

from lrucache import LRU


class LRUTest(unittest.TestCase):
    def test_evicts_the_least_recently_used(self):
        c = LRU(2)
        c.put("a", 1)
        c.put("b", 2)
        c.put("c", 3)
        self.assertEqual((c.get("a"), c.keys()), (None, ["b", "c"]))

    def test_a_read_refreshes_recency(self):
        c = LRU(2)
        c.put("a", 1)
        c.put("b", 2)
        self.assertEqual(c.get("a"), 1)
        c.put("c", 3)
        self.assertEqual(c.keys(), ["a", "c"])

    def test_replacing_a_value_refreshes_recency(self):
        c = LRU(2)
        c.put("a", 1)
        c.put("b", 2)
        c.put("a", 10)
        c.put("c", 3)
        self.assertEqual((c.get("a"), c.get("b")), (10, None))


if __name__ == "__main__":
    unittest.main()
''',
)

PACKAGES["csvline"] = (
    '''"""One-line CSV parsing.

parse_line(line) splits on commas and returns the fields as strings. A field
may be wrapped in double quotes: commas inside do not split, and a doubled
quote ("") stands for one literal quote. Whitespace around a field, outside
its quotes, is stripped. The last field is always returned.
"""


def parse_line(line):
    fields, cur, quoted, i = [], [], False, 0
    while i < len(line):
        ch = line[i]
        if quoted:
            if ch == '"':
                if line[i + 1:i + 2] == '"':
                    cur.append('"')
                    i += 1
                else:
                    quoted = False
            else:
                cur.append(ch)
        elif ch == '"':
            quoted = True
        elif ch == ",":
            fields.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
        i += 1
    fields.append("".join(cur).strip())
    return fields
''',
    [
        ("if line[i + 1:i + 2] == '\"':", "if line[i - 1:i] == '\"':"),
        ("    fields.append(\"\".join(cur).strip())\n    return fields\n", "    return fields\n"),
    ],
    '''import unittest

from csvline import parse_line


class CsvLineTest(unittest.TestCase):
    def test_plain_fields(self):
        self.assertEqual(parse_line("a,b,c"), ["a", "b", "c"])

    def test_the_last_field_is_returned(self):
        self.assertEqual(parse_line("x,y"), ["x", "y"])
        self.assertEqual(parse_line("only"), ["only"])

    def test_commas_inside_quotes_do_not_split(self):
        self.assertEqual(parse_line('a, "b, c" ,d'), ["a", "b, c", "d"])

    def test_a_doubled_quote_is_one_literal_quote(self):
        self.assertEqual(parse_line('"say ""hi""",z'), ['say "hi"', "z"])

    def test_empty_fields_are_kept(self):
        self.assertEqual(parse_line("a,,c,"), ["a", "", "c", ""])


if __name__ == "__main__":
    unittest.main()
''',
)


def render(name: str, defects=None) -> str:
    """The module source with the given defect indexes applied (default: all)."""
    source, subs, _ = PACKAGES[name]
    for i in range(len(subs)) if defects is None else defects:
        good, bad = subs[i]
        if source.count(good) != 1:
            raise ValueError(f"{name}: defect {i} must match exactly once")
        source = source.replace(good, bad)
    return source


def main(out: Path, fixed: bool = False, slow_s: int = 0) -> None:
    for name, (_, subs, tests) in PACKAGES.items():
        root = out / "pkgs" / name
        root.mkdir(parents=True, exist_ok=True)
        (root / f"{name}.py").write_text(render(name, [] if fixed else None))
        pause = f"import time\n\ntime.sleep({slow_s})  # stands in for a slow fixture\n" if slow_s else ""
        (root / f"test_{name}.py").write_text(pause + tests)


if __name__ == "__main__":
    main(Path(sys.argv[1]), fixed="--fixed" in sys.argv[2:])
