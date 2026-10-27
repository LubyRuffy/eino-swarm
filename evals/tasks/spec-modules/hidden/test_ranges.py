import unittest

from ranges import format_ranges, parse_ranges


class Rules(unittest.TestCase):
    def test_rule1_items(self):
        self.assertEqual(parse_ranges("1-3,5", 10), [1, 2, 3, 5])
        self.assertEqual(parse_ranges("7", 10), [7])

    def test_rule2_whitespace(self):
        self.assertEqual(parse_ranges(" 1 - 3 , 5 ", 10), [1, 2, 3, 5])
        self.assertEqual(parse_ranges("2\t-\t4", 10), [2, 3, 4])

    def test_rule3_sorted_unique(self):
        self.assertEqual(parse_ranges("5,1-3,2", 10), [1, 2, 3, 5])
        self.assertEqual(parse_ranges("3,3,3", 10), [3])
        self.assertEqual(parse_ranges("1-5,3-7", 10), [1, 2, 3, 4, 5, 6, 7])

    def test_rule4_open_ends(self):
        self.assertEqual(parse_ranges("7-", 10), [7, 8, 9, 10])
        self.assertEqual(parse_ranges("-3", 10), [1, 2, 3])
        self.assertEqual(parse_ranges("-2,9-", 10), [1, 2, 9, 10])
        self.assertEqual(parse_ranges("10-", 10), [10])
        with self.assertRaises(ValueError):
            parse_ranges("-", 10)
        with self.assertRaises(ValueError):
            parse_ranges(" - ", 10)

    def test_rule5_out_of_range_is_an_error(self):
        for bad in ("1-99", "11", "0-3", "5-11", "99-"):
            with self.assertRaises(ValueError, msg=bad):
                parse_ranges(bad, 10)
        self.assertEqual(parse_ranges("1-10", 10), list(range(1, 11)))

    def test_rule6_malformed(self):
        for bad in ("0", "5-3", "1,,2", "1,2,", ",1", "1a", "1.5", "1;2", "1-2-3", "-0", "--3", "1 2"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse_ranges(bad, 10)
        self.assertEqual(parse_ranges("", 10), [])
        self.assertEqual(parse_ranges("   ", 10), [])
        self.assertEqual(parse_ranges("3-3", 10), [3])

    def test_rule7_types(self):
        for bad in (None, 5, b"1", ["1"]):
            with self.assertRaises(TypeError, msg=repr(bad)):
                parse_ranges(bad, 10)
        for bad in (0, -1, 1.5, "10", None, True):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse_ranges("1", bad)
        self.assertEqual(parse_ranges("1", 1), [1])

    def test_rule8_format_runs(self):
        self.assertEqual(format_ranges([1, 2, 3, 5, 7, 8, 9]), "1-3,5,7-9")
        self.assertEqual(format_ranges([9, 7, 8, 1, 3, 2, 5]), "1-3,5,7-9")
        self.assertEqual(format_ranges([1, 1, 2, 3, 3]), "1-3")
        self.assertEqual(format_ranges(iter([4, 5, 6, 7])), "4-7")
        for bad in ([0], [-1], [1.5], ["1"], [True], [1, None]):
            with self.assertRaises(ValueError, msg=repr(bad)):
                format_ranges(bad)

    def test_rule9_pairs_and_empty(self):
        self.assertEqual(format_ranges([1, 2, 5]), "1,2,5")
        self.assertEqual(format_ranges([1, 2, 4, 5]), "1,2,4,5")
        self.assertEqual(format_ranges([]), "")
        self.assertEqual(format_ranges([3]), "3")
        self.assertEqual(format_ranges([1, 2, 3, 5, 6]), "1-3,5,6")

    def test_round_trip(self):
        for pages in ([1, 2, 3], [1, 3, 5], [2, 3, 4, 8, 9], [10]):
            self.assertEqual(parse_ranges(format_ranges(pages), 10), pages)


if __name__ == "__main__":
    unittest.main()
