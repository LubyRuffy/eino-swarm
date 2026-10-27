"""One test per numbered rule of duration/README.md. Every case is one the
README states or follows directly from a rule it states."""

import unittest

from duration import format_duration, parse_duration

LIMIT = 315360000


class Rules(unittest.TestCase):
    def ok(self, cases):
        for text, want in cases:
            with self.subTest(text=text):
                self.assertEqual(parse_duration(text), want)

    def bad(self, texts, exc=ValueError):
        for text in texts:
            with self.subTest(text=text):
                with self.assertRaises(exc):
                    parse_duration(text)

    def test_rule01_units(self):
        self.ok([("1h30m", 5400), ("2d", 172800), ("45s", 45), ("3m", 180)])
        self.bad(["1H", "1D", "1M", "2S", "1x", "5"])

    def test_rule02_order(self):
        self.ok([("1d5s", 86405), ("1d2h3m4s", 93784), ("2h4s", 7204)])
        self.bad(["30m1h", "1h1h", "1s1d", "1m1m"])

    def test_rule03_ascii_digits(self):
        self.bad(["1_000s", "\uff11h", "\u0663s", "1\u0663m", "01h", "00s", "1h05m"])

    def test_rule04_zero(self):
        self.ok([("0s", 0)])
        self.bad(["0h30m", "1h0s", "0.0m", "0m", "0d"])

    def test_rule05_fraction(self):
        self.ok([("1.5h", 5400), ("0.5h", 1800), ("1h1.5m", 3690), ("1.5m", 90), ("2.25m", 135)])
        self.bad(["1.5h30m", "1.h", ".5h", "1.2345s", "0.5s", "1.001m"])

    def test_rule06_sign(self):
        self.ok([("-1h30m", -5400), ("-45s", -45)])
        self.bad(["+1h", "--1h", "1h-30m", "-0s", "-"])

    def test_rule07_iso(self):
        self.ok([("PT1H30M", 5400), ("P2D", 172800), ("P1DT5S", 86405), ("PT0S", 0),
                 ("-PT1H", -3600), ("PT1.5H", 5400)])
        self.bad(["P", "PT", "P1DT", "PT1h", "pt1h", "PT0H", "P1DT1.5H30M", "-PT0S",
                  "PT30M1H", "P1H", "PT1D", "PT01H"])

    def test_rule08_whitespace(self):
        self.bad([" 1h", "1h ", "1h 30m", "", "\t1h", "1h\n"])

    def test_rule09_limit(self):
        self.ok([("3650d", LIMIT), ("-3650d", -LIMIT)])
        self.bad(["3650d1s", "-3650d1s", "P3651D"], OverflowError)
        # Any number of digits is still a number; Python's int/str conversion
        # limit (4300 digits by default) is not one of the rules.
        self.bad(["9" * 5000 + "s", "-" + "9" * 5000 + "d", "PT" + "9" * 5000 + "M"], OverflowError)

    def test_rule10_type(self):
        for value in (None, 5400, b"1h", 1.5):
            with self.subTest(value=value):
                with self.assertRaises(TypeError):
                    parse_duration(value)

    def test_rule11_format(self):
        for n, want in ((5400, "1h30m"), (86405, "1d5s"), (0, "0s"), (-90, "-1m30s"),
                        (93784, "1d2h3m4s"), (3600, "1h"), (LIMIT, "3650d")):
            with self.subTest(n=n):
                self.assertEqual(format_duration(n), want)

    def test_rule12_format_types(self):
        for value in (True, False, 1.0, "60", None):
            with self.subTest(value=value):
                with self.assertRaises(TypeError):
                    format_duration(value)
        for value in (LIMIT + 1, -LIMIT - 1):
            with self.subTest(value=value):
                with self.assertRaises(OverflowError):
                    format_duration(value)

    def test_rule13_round_trip(self):
        for n in (0, 1, 59, 60, 61, 3599, 3600, 86399, 86400, 90061, -7322, LIMIT, -LIMIT):
            with self.subTest(n=n):
                self.assertEqual(parse_duration(format_duration(n)), n)


if __name__ == "__main__":
    unittest.main()
