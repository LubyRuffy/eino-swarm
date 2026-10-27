import unittest

from textkit import norm_ws, norm_ws_count, slugify, title_case, wrap


class TextkitTest(unittest.TestCase):
    def test_collapses_whitespace(self):
        self.assertEqual(norm_ws("  a \t b\n\nc  "), "a b c")

    def test_counts_runs(self):
        self.assertEqual(norm_ws_count(" a  b c "), 2)

    def test_slugify(self):
        self.assertEqual(slugify("  Hello,   World! "), "hello-world")

    def test_title_case(self):
        self.assertEqual(title_case("the  art of   war"), "The Art of War")

    def test_wrap(self):
        self.assertEqual(wrap("one two  three four", 9), ["one two", "three", "four"])
