import unittest

from slug import slugify


class Rules(unittest.TestCase):
    def test_rule1_runs_become_one_separator(self):
        self.assertEqual(slugify("Hello, World!"), "hello-world")
        self.assertEqual(slugify("  --a__b..c--  "), "a-b-c")
        self.assertEqual(slugify("Version 2.0 / Final"), "version-2-0-final")

    def test_rule2_apostrophes_leave_no_separator(self):
        self.assertEqual(slugify("Don't Stop"), "dont-stop")
        self.assertEqual(slugify("It\u2019s Here"), "its-here")
        self.assertEqual(slugify("Rock 'n' Roll"), "rock-n-roll")

    def test_rule3_ampersand_is_and(self):
        self.assertEqual(slugify("R&D"), "r-and-d")
        self.assertEqual(slugify("Salt & Pepper"), "salt-and-pepper")

    def test_rule4_letters_without_accents(self):
        self.assertEqual(slugify("Crème Brûlée"), "creme-brulee")
        self.assertEqual(slugify("Straße"), "strasse")
        self.assertEqual(slugify("Æsir Øl"), "aesir-ol")
        self.assertEqual(slugify("Łódź"), "lodz")
        self.assertEqual(slugify("东京 Tower"), "tower")

    def test_rule5_digit_prefix_counts_toward_the_limit(self):
        self.assertEqual(slugify("2024 Plan"), "n-2024-plan")
        self.assertEqual(slugify("2024 Annual Planning Review", max_len=16), "n-2024-annual")
        self.assertEqual(slugify("12345678 abc", max_len=10), "n-12345678")

    def test_rule6_whole_words_within_the_limit(self):
        self.assertEqual(slugify("alpha beta gamma delta", max_len=16), "alpha-beta-gamma")
        self.assertEqual(slugify("alpha beta gamma delta", max_len=15), "alpha-beta")
        self.assertEqual(slugify("abcdefghijklmnop q", max_len=8), "abcdefgh")
        self.assertEqual(len(slugify("word " * 20)), 29)
        self.assertEqual(slugify("exactly eight"), "exactly-eight")

    def test_rule7_empty_is_untitled(self):
        self.assertEqual(slugify(""), "untitled")
        self.assertEqual(slugify("!!! ???"), "untitled")
        self.assertEqual(slugify("''"), "untitled")

    def test_rule8_arguments(self):
        with self.assertRaises(ValueError):
            slugify("abc", max_len=7)
        self.assertEqual(slugify("abc", max_len=8), "abc")
        for bad in (None, 5, b"abc"):
            with self.assertRaises(TypeError, msg=repr(bad)):
                slugify(bad)
        # The README says nothing about bool here (unlike money's rule 8),
        # so either answer for max_len=True meets it.
        for bad in (8.0, "16", None):
            with self.assertRaises(TypeError, msg=repr(bad)):
                slugify("abc", max_len=bad)


if __name__ == "__main__":
    unittest.main()
