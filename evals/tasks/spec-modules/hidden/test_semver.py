import unittest

from semver import compare, parse, sort_versions


class Rules(unittest.TestCase):
    def test_rule1_parse_shape(self):
        self.assertEqual(parse("1.2.3"), (1, 2, 3, ()))
        self.assertEqual(parse("1.2.3-alpha.1"), (1, 2, 3, ("alpha", 1)))
        self.assertEqual(parse("0.0.0-0.x.10"), (0, 0, 0, (0, "x", 10)))

    def test_rule2_v_prefix_and_whitespace(self):
        self.assertEqual(parse("v1.2.3"), (1, 2, 3, ()))
        self.assertEqual(parse("  1.2.3\n"), (1, 2, 3, ()))
        for bad in ("V1.2.3", "vv1.2.3", "1. 2.3", "1.2 .3", "v 1.2.3"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse(bad)

    def test_rule3_build_metadata_dropped(self):
        self.assertEqual(parse("1.2.3+build5"), (1, 2, 3, ()))
        self.assertEqual(parse("1.2.3-rc.1+b.7"), (1, 2, 3, ("rc", 1)))

    def test_rule4_shape_errors(self):
        for bad in ("1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "1.0.0-01",
                    "1.0.0-", "1.0.0-a..b", "1.0.0-.a", "", "a.b.c", "1.2.x"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                parse(bad)
        self.assertEqual(parse("0.0.0"), (0, 0, 0, ()))
        self.assertEqual(parse("1.0.0-0a"), (1, 0, 0, ("0a",)))
        self.assertEqual(parse("1.0.0-0"), (1, 0, 0, (0,)))
        self.assertEqual(parse("1.0.0-a-b"), (1, 0, 0, ("a-b",)))

    def test_rule5_numeric_core(self):
        self.assertEqual(compare("1.10.0", "1.9.0"), 1)
        self.assertEqual(compare("1.9.0", "1.10.0"), -1)
        self.assertEqual(compare("2.0.0", "10.0.0"), -1)
        self.assertEqual(compare("1.2.3", "1.2.3"), 0)
        self.assertEqual(compare("1.2.10", "1.2.9"), 1)

    def test_rule6_prerelease_below_release(self):
        self.assertEqual(compare("1.0.0-rc.1", "1.0.0"), -1)
        self.assertEqual(compare("1.0.0", "1.0.0-rc.1"), 1)
        self.assertEqual(compare("1.0.1-rc.1", "1.0.0"), 1)

    def test_rule7_prerelease_order(self):
        self.assertEqual(compare("1.0.0-alpha.2", "1.0.0-alpha.10"), -1)
        self.assertEqual(compare("1.0.0-Alpha", "1.0.0-alpha"), -1)
        self.assertEqual(compare("1.0.0-1", "1.0.0-alpha"), -1)
        self.assertEqual(compare("1.0.0-alpha", "1.0.0-alpha.1"), -1)
        self.assertEqual(compare("1.0.0-alpha.1", "1.0.0-alpha"), 1)
        self.assertEqual(compare("1.0.0-alpha.beta", "1.0.0-alpha.1"), 1)
        self.assertEqual(compare("1.0.0-beta", "1.0.0-alpha"), 1)
        self.assertEqual(compare("1.0.0-rc.1", "1.0.0-rc.1"), 0)

    def test_rule8_build_metadata_ignored(self):
        self.assertEqual(compare("1.0.0+a", "1.0.0+b"), 0)
        self.assertEqual(compare("1.0.0-rc.1+x", "1.0.0-rc.1"), 0)
        self.assertEqual(compare("1.0.0+z", "1.0.1"), -1)

    def test_rule9_sort(self):
        got = sort_versions(["1.0.0", "1.0.0-rc.1", "0.9.0", "1.0.0-alpha", "v1.0.0-alpha.1"])
        self.assertEqual(got, ["0.9.0", "1.0.0-alpha", "v1.0.0-alpha.1", "1.0.0-rc.1", "1.0.0"])

    def test_rule9_sort_is_stable_for_equal_versions(self):
        self.assertEqual(sort_versions(["v1.0.0", "1.0.0+b", "1.0.0"]), ["v1.0.0", "1.0.0+b", "1.0.0"])
        self.assertEqual(sort_versions(["1.0.0", "v1.0.0"]), ["1.0.0", "v1.0.0"])

    def test_rule9_sort_validates_every_item(self):
        with self.assertRaises(ValueError):
            sort_versions(["not-a-version"])
        with self.assertRaises(ValueError):
            sort_versions(["1.0.0", "1.2"])
        self.assertEqual(sort_versions([]), [])

    def test_type_errors(self):
        for bad in (1, None, b"1.0.0"):
            with self.assertRaises(TypeError, msg=repr(bad)):
                parse(bad)
        with self.assertRaises(TypeError):
            sort_versions([1])


if __name__ == "__main__":
    unittest.main()
