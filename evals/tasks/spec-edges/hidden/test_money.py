import unittest

from money import format_amount, parse_amount


class Rules(unittest.TestCase):
    def test_rule1_decimals_and_half_away_rounding(self):
        self.assertEqual(parse_amount("12"), 1200)
        self.assertEqual(parse_amount("12.5"), 1250)
        self.assertEqual(parse_amount("12.34"), 1234)
        self.assertEqual(parse_amount("0.125"), 13)
        self.assertEqual(parse_amount("0.005"), 1)
        self.assertEqual(parse_amount("1.234"), 123)
        self.assertEqual(parse_amount("2.675"), 268)
        self.assertEqual(parse_amount("0.015"), 2)
        for bad in ("1.2345", ".5", "5.", "1.2.3", "abc", "1e3"):
            with self.assertRaises(ValueError, msg=bad):
                parse_amount(bad)

    def test_rule1_any_number_of_digits(self):
        # The README puts no bound on the digits; Python's int/str conversion
        # limit (4300 digits by default) is not one of the rules.
        ones = (10 ** 5000 - 1) // 9
        self.assertEqual(parse_amount("1" * 5000), ones * 100)
        self.assertEqual(parse_amount("1" * 5000 + ".5"), ones * 100 + 50)
        text = format_amount(10 ** 5000)
        self.assertEqual(text, "1" + ",000" * 1666 + ".00")
        self.assertEqual(parse_amount(text), 10 ** 5000)

    def test_rule2_thousands_separators(self):
        self.assertEqual(parse_amount("1,234.5"), 123450)
        self.assertEqual(parse_amount("12,345,678"), 1234567800)
        self.assertEqual(parse_amount("1234567"), 123456700)
        self.assertEqual(parse_amount("999,999.99"), 99999999)
        for bad in ("12,34", "1,2345", "1234,567", ",123", "1,,234", "1,234,"):
            with self.assertRaises(ValueError, msg=bad):
                parse_amount(bad)

    def test_rule3_negative_forms(self):
        self.assertEqual(parse_amount("-12.00"), -1200)
        self.assertEqual(parse_amount("(12.00)"), -1200)
        self.assertEqual(parse_amount("(1,234.5)"), -123450)
        self.assertEqual(parse_amount("-0.125"), -13)
        self.assertEqual(parse_amount("(0.125)"), -13)
        for bad in ("-(12.00)", "(-12.00)", "+5", "(12.00", "12.00)", "()", "--1"):
            with self.assertRaises(ValueError, msg=bad):
                parse_amount(bad)

    def test_rule4_whitespace(self):
        self.assertEqual(parse_amount("  3.5\t"), 350)
        for bad in ("", "   ", "1 234", "- 5", "( 5)", "1. 5"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse_amount(bad)

    def test_rule5_type(self):
        for bad in (12, 1.5, None, b"1.00"):
            with self.assertRaises(TypeError, msg=repr(bad)):
                parse_amount(bad)

    def test_rule6_separators_from_five_digits(self):
        self.assertEqual(format_amount(1234), "12.34")
        self.assertEqual(format_amount(123456), "1234.56")
        self.assertEqual(format_amount(999999), "9999.99")
        self.assertEqual(format_amount(1000000), "10,000.00")
        self.assertEqual(format_amount(1234567), "12,345.67")
        self.assertEqual(format_amount(100000000), "1,000,000.00")
        self.assertEqual(format_amount(7), "0.07")

    def test_rule7_negative_in_parentheses(self):
        self.assertEqual(format_amount(-150), "(1.50)")
        self.assertEqual(format_amount(-1234567), "(12,345.67)")
        self.assertEqual(format_amount(-5), "(0.05)")
        self.assertEqual(format_amount(0), "0.00")

    def test_rule8_type(self):
        for bad in (True, False, 1.5, "100", None):
            with self.assertRaises(TypeError, msg=repr(bad)):
                format_amount(bad)


if __name__ == "__main__":
    unittest.main()
