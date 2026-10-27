import unittest

from durations import format_duration, parse_duration


class Rules(unittest.TestCase):
    def test_rule1_parts_and_units(self):
        self.assertEqual(parse_duration("1h30m"), 5400)
        self.assertEqual(parse_duration("90s"), 90)
        self.assertEqual(parse_duration("2d4h"), 2 * 86400 + 4 * 3600)
        self.assertEqual(parse_duration("1d1h1m1s"), 90061)

    def test_rule2_case_insensitive(self):
        self.assertEqual(parse_duration("1H30M"), 5400)
        self.assertEqual(parse_duration("2D"), 172800)

    def test_rule3_whitespace(self):
        self.assertEqual(parse_duration(" 1h 30m "), 5400)
        self.assertEqual(parse_duration("1h\t30m"), 5400)
        for bad in ("1 h", "1h 3 0m", "1 h30m"):
            with self.assertRaises(ValueError, msg=bad):
                parse_duration(bad)

    def test_rule4_order_and_repeats(self):
        for bad in ("30m1h", "1h1h", "1s1m", "1d1d"):
            with self.assertRaises(ValueError, msg=bad):
                parse_duration(bad)

    def test_rule5_zero_and_signs(self):
        self.assertEqual(parse_duration("0s"), 0)
        self.assertEqual(parse_duration("0m"), 0)
        self.assertEqual(parse_duration("0h5s"), 5)
        for bad in ("-5s", "+5s", "1h-5m"):
            with self.assertRaises(ValueError, msg=bad):
                parse_duration(bad)

    def test_rule6_invalid_text(self):
        for bad in ("", "   ", "90", "5w", "h", "1h30", "1.5h", "abc"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse_duration(bad)

    def test_rule7_type_error(self):
        for bad in (90, None, b"1h", 1.5, ["1h"]):
            with self.assertRaises(TypeError, msg=repr(bad)):
                parse_duration(bad)

    def test_rule8_format(self):
        self.assertEqual(format_duration(5400), "1h30m")
        self.assertEqual(format_duration(86400), "1d")
        self.assertEqual(format_duration(90061), "1d1h1m1s")
        self.assertEqual(format_duration(3601), "1h1s")
        self.assertEqual(format_duration(172800), "2d")
        self.assertEqual(format_duration(59), "59s")

    def test_rule9_zero_and_bad_input(self):
        self.assertEqual(format_duration(0), "0s")
        for bad in (-1, 1.5, "60", None, True, False):
            with self.assertRaises(ValueError, msg=repr(bad)):
                format_duration(bad)

    def test_round_trip(self):
        for n in (0, 1, 59, 60, 3599, 3600, 86399, 86400, 90061, 1234567):
            self.assertEqual(parse_duration(format_duration(n)), n)


if __name__ == "__main__":
    unittest.main()
