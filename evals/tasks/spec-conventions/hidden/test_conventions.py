"""CONVENTIONS.md, checked for the module named in $SPEC_MODULE."""

import importlib
import inspect
import os
import unittest

MODULE = os.environ["SPEC_MODULE"]

PUBLIC = {
    "money": ["parse_amount", "format_amount"],
    "slug": ["slugify"],
    "headers": ["normalize_headers"],
}

# (function, args, kwargs) that each README says must raise.
BAD_INPUT = {
    "money": [("parse_amount", ("",), {}), ("parse_amount", ("abc",), {}), ("parse_amount", ("1.2345",), {}),
              ("parse_amount", (12,), {}), ("format_amount", (1.5,), {}), ("format_amount", (True,), {})],
    "slug": [("slugify", (None,), {}), ("slugify", ("abc",), {"max_len": 7}), ("slugify", ("abc",), {"max_len": "16"})],
    "headers": [("normalize_headers", ("a,b",), {}), ("normalize_headers", (["a", 3],), {})],
}


def raised(mod):
    for name, args, kwargs in BAD_INPUT[MODULE]:
        try:
            getattr(mod, name)(*args, **kwargs)
        except (TypeError, ValueError) as exc:
            yield name, args, kwargs, exc
        else:
            raise AssertionError("%s%r %r did not raise" % (name, args, kwargs))


class Conventions(unittest.TestCase):
    def setUp(self):
        self.mod = importlib.import_module(MODULE)

    def test_conv1_all_lists_the_public_functions(self):
        self.assertEqual(sorted(getattr(self.mod, "__all__", [])), sorted(PUBLIC[MODULE]))

    def test_conv2_docstrings(self):
        for name in PUBLIC[MODULE]:
            self.assertTrue((getattr(self.mod, name).__doc__ or "").strip(), name)

    def test_conv3_options_are_keyword_only(self):
        for name in PUBLIC[MODULE]:
            for p in inspect.signature(getattr(self.mod, name)).parameters.values():
                if p.default is not inspect.Parameter.empty:
                    self.assertEqual(p.kind, inspect.Parameter.KEYWORD_ONLY, "%s(%s)" % (name, p.name))

    def test_conv4_messages_name_the_function(self):
        for name, args, kwargs, exc in raised(self.mod):
            self.assertTrue(str(exc).startswith(name + ":"), "%s%r %r -> %r" % (name, args, kwargs, str(exc)))

    def test_conv5_no_internal_context(self):
        for name, args, kwargs, exc in raised(self.mod):
            self.assertTrue(exc.__context__ is None or exc.__suppress_context__,
                            "%s%r %r carries %r" % (name, args, kwargs, exc.__context__))


if __name__ == "__main__":
    unittest.main()
