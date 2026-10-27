import unittest

from headers import normalize_headers


class Rules(unittest.TestCase):
    def test_rule1_normalized_names(self):
        self.assertEqual(normalize_headers([" Unit Price ($) "]), ["unit_price"])
        self.assertEqual(normalize_headers(["First-Name", "e.mail"]), ["first_name", "e_mail"])
        self.assertEqual(normalize_headers(["Größe"]), ["gr_e"])

    def test_rule2_digit_prefix(self):
        self.assertEqual(normalize_headers(["2024 Total"]), ["c_2024_total"])
        self.assertEqual(normalize_headers(["Q1 2024"]), ["q1_2024"])

    def test_rule3_empty_names_use_their_position(self):
        self.assertEqual(normalize_headers(["a", "", "b"]), ["a", "col_2", "b"])
        self.assertEqual(normalize_headers(["  ", "($)"]), ["col_1", "col_2"])

    def test_rule4_repeats_are_numbered_from_two(self):
        self.assertEqual(normalize_headers(["a", "a", "a"]), ["a", "a_2", "a_3"])
        self.assertEqual(normalize_headers(["Name", "name ", "NAME"]), ["name", "name_2", "name_3"])

    def test_rule5_existing_names_take_precedence(self):
        self.assertEqual(normalize_headers(["a", "a", "a_2"]), ["a", "a_3", "a_2"])
        self.assertEqual(normalize_headers(["", "col_1"]), ["col_1_2", "col_1"])
        self.assertEqual(normalize_headers(["x", "", "col_2"]), ["x", "col_2_2", "col_2"])
        out = normalize_headers(["id", "ID", "id_2", "id 3", "id"])
        self.assertEqual(out, ["id", "id_4", "id_2", "id_3", "id_5"])
        self.assertEqual(len(set(out)), len(out))
        # Column 3 has col_3 after rule 3, so the repeated col skips it too.
        self.assertEqual(normalize_headers(["col", "col", "", "col_2"]), ["col", "col_4", "col_3", "col_2"])

    def test_rule6_inputs(self):
        self.assertEqual(normalize_headers([]), [])
        self.assertEqual(normalize_headers(("a", "b")), ["a", "b"])
        for bad in ("a,b", None, {"a": 1}, iter(["a"])):
            with self.assertRaises(TypeError, msg=repr(bad)):
                normalize_headers(bad)
        with self.assertRaises(TypeError):
            normalize_headers(["a", 3])


if __name__ == "__main__":
    unittest.main()
