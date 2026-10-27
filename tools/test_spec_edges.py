"""Offline checks of the review fixtures after spec-modules (spec-edges,
spec-conventions, spec-dense): the hidden rules are satisfiable, hidden from
the workspace, and miss what the usual library call or habit writes. Without
that last property a fixture would repeat spec-modules, where no unreviewed
run had a mistake for a review to catch.
"""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402

TASK = agent_eval.TASKS_DIR / "spec-edges"
MODULES = ("money", "slug", "headers")

# What a careful but habit-driven implementation writes: it passes the visible
# tests and reads plausibly against the README, yet uses float rounding,
# locale-style separators, NFKD alone and suffix-on-collision.
HABITUAL = {
    "money": '''import re
def parse_amount(text):
    if not isinstance(text, str):
        raise TypeError("text")
    s = text.strip().replace(",", "")
    neg = s.startswith("-") or (s.startswith("(") and s.endswith(")"))
    s = s.strip("-()")
    if not re.fullmatch(r"[0-9]+(\\.[0-9]{1,3})?", s):
        raise ValueError(text)
    cents = round(float(s) * 100)
    return -cents if neg else cents
def format_amount(cents):
    if not isinstance(cents, int):
        raise TypeError("cents")
    out = "{:,.2f}".format(abs(cents) / 100)
    return "-" + out if cents < 0 else out
''',
    "slug": '''import re, unicodedata
def slugify(text, max_len=32):
    if not isinstance(text, str):
        raise TypeError("text")
    if max_len < 8:
        raise ValueError("max_len")
    s = unicodedata.normalize("NFKD", text).encode("ascii", "ignore").decode().lower()
    s = re.sub(r"[^a-z0-9]+", "-", s).strip("-")
    return s[:max_len].rstrip("-") or "untitled"
''',
    "headers": '''import re
def normalize_headers(names):
    out, seen = [], {}
    for i, n in enumerate(names):
        s = re.sub(r"[^a-z0-9]+", "_", n.strip().lower()).strip("_") or "col_%d" % (i + 1)
        if s in seen:
            seen[s] += 1
            s = "%s_%d" % (s, seen[s])
        else:
            seen[s] = 1
        out.append(s)
    return out
''',
}


def hidden(ws, module):
    return subprocess.run([sys.executable, str(TASK / "hidden" / "run_hidden.py"), module], cwd=ws,
                          capture_output=True, text=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))


class SpecEdgesTest(unittest.TestCase):
    def prepared(self, task_id="spec-edges"):
        d = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, d)
        agent_eval.prepare_workspace(grade.load_task(agent_eval.TASKS_DIR / task_id), d)
        return d

    def test_reference_meets_every_hidden_rule(self):
        task = grade.load_task(TASK)
        untouched = self.prepared()
        fixed = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, fixed)
        subprocess.run([sys.executable, str(TASK / "generate.py"), str(fixed), "--fixed"], check=True)
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), fixed, "", [],
                                   grade.hash_tree(untouched))
        self.assertTrue(all(r["ok"] for r in results), [r for r in results if not r["ok"]])

    def test_starts_without_a_solution_and_hides_its_rule_tests(self):
        task = grade.load_task(TASK)
        ws = self.prepared()
        names = sorted(p.relative_to(ws).as_posix() for p in ws.rglob("*") if p.is_file())
        self.assertEqual(names, sorted(f"{m}/{f}" for m in MODULES for f in ("README.md", "test_basic.py")))
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), ws, "", [],
                                   grade.hash_tree(ws))
        self.assertEqual([r["ok"] for r in results[:3]], [False] * 3)
        self.assertTrue(all(r["ok"] for r in results[3:]))
        self.assertIn("failed rules: import", hidden(ws, "money").stdout)

    def test_habitual_solutions_pass_the_visible_tests_and_miss_rules(self):
        for module, source in HABITUAL.items():
            ws = self.prepared()
            (ws / module / f"{module}.py").write_text(source)
            basic = subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", module, "-q"], cwd=ws,
                                   capture_output=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
            self.assertEqual(basic.returncode, 0, f"{module}: must satisfy the visible tests")
            out = hidden(ws, module)
            self.assertEqual(out.returncode, 1, module)
            failed = out.stdout.strip().splitlines()[-1].removeprefix("failed rules: ").split(",")
            self.assertGreaterEqual(len(failed), 3, f"{module}: only {failed} go against habit")

    def test_arms_differ_only_in_the_request_for_a_check(self):
        base = grade.load_task(TASK)
        selfreview = grade.load_task(agent_eval.TASKS_DIR / "spec-edges-selfreview")
        independent = grade.load_task(agent_eval.TASKS_DIR / "spec-edges-review")
        files = lambda ws: {p.relative_to(ws).as_posix(): p.read_bytes() for p in ws.rglob("*") if p.is_file()}
        reference = files(self.prepared())
        self.assertEqual(files(self.prepared("spec-edges-selfreview")), reference)
        self.assertEqual(files(self.prepared("spec-edges-review")), reference)
        for arm in (selfreview, independent):
            self.assertTrue(arm["prompt"].startswith(base["prompt"]))
            self.assertGreater(len(arm["prompt"]), len(base["prompt"]))
        self.assertIn("worker", independent["prompt"])
        self.assertNotIn("worker", selfreview["prompt"])
        self.assertEqual(selfreview["checks"], base["checks"])
        self.assertEqual(independent["checks"][:-1], base["checks"])
        self.assertEqual(independent["checks"][-1]["tool"], "spawn_agent")

    def test_the_review_requests_match_the_first_triple(self):
        # Same wording as spec-modules, so a difference between the triples
        # comes from the rules, not from how review was asked for.
        for arm in ("selfreview", "review"):
            first = grade.load_task(agent_eval.TASKS_DIR / f"spec-modules-{arm}")["prompt"]
            first_base = grade.load_task(agent_eval.TASKS_DIR / "spec-modules")["prompt"]
            second = grade.load_task(agent_eval.TASKS_DIR / f"spec-edges-{arm}")["prompt"]
            self.assertEqual(second[len(grade.load_task(TASK)["prompt"]):], first[len(first_base):])


CONVENTIONS_TASK = agent_eval.TASKS_DIR / "spec-conventions"


class SpecConventionsTest(unittest.TestCase):
    """The third triple: spec-edges plus repository-wide conventions. Neither
    model missed a spec-edges rule unreviewed, so the headroom has to come
    from rules a single pass skips, not from harder module rules."""

    def prepared(self, task_id="spec-conventions", fixed=False):
        d = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, d)
        task = grade.load_task(agent_eval.TASKS_DIR / task_id)
        if fixed:
            subprocess.run([sys.executable, str(task["dir"] / "generate.py"), str(d), "--fixed"], check=True)
        else:
            agent_eval.prepare_workspace(task, d)
        return d

    def hidden(self, ws, module):
        return subprocess.run([sys.executable, str(CONVENTIONS_TASK / "hidden" / "run_hidden.py"), module], cwd=ws,
                              capture_output=True, text=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))

    def test_reference_meets_every_rule_and_convention(self):
        task = grade.load_task(CONVENTIONS_TASK)
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), self.prepared(fixed=True), "",
                                   [], grade.hash_tree(self.prepared()))
        self.assertTrue(all(r["ok"] for r in results), [r for r in results if not r["ok"]])

    def test_every_readme_points_to_the_conventions(self):
        ws = self.prepared()
        self.assertTrue((ws / "CONVENTIONS.md").is_file())
        for m in MODULES:
            self.assertIn("../CONVENTIONS.md", (ws / m / "README.md").read_text())
            self.assertFalse((ws / m / f"{m}.py").exists())

    def test_conventions_are_beyond_the_readme_rules(self):
        # A solution that meets every README rule still misses the conventions.
        ws = self.prepared("spec-edges", fixed=True)
        for m in MODULES:
            out = self.hidden(ws, m)
            failed = out.stdout.strip().splitlines()[-1].removeprefix("failed rules: ").split(",")
            self.assertTrue(all(f.startswith("test_conv") for f in failed), failed)
            self.assertGreaterEqual(len(failed), 3, f"{m}: {failed}")

    def test_arms_share_the_spec_edges_prompts(self):
        for suffix in ("", "-selfreview", "-review"):
            arm = grade.load_task(agent_eval.TASKS_DIR / f"spec-conventions{suffix}")
            self.assertEqual(arm["prompt"], grade.load_task(agent_eval.TASKS_DIR / f"spec-edges{suffix}")["prompt"])
            self.assertEqual(arm["checks"][:10], grade.load_task(CONVENTIONS_TASK)["checks"])
        files = lambda ws: {p.relative_to(ws).as_posix(): p.read_bytes() for p in ws.rglob("*") if p.is_file()}
        self.assertEqual(files(self.prepared("spec-conventions-review")), files(self.prepared()))


DENSE_TASK = agent_eval.TASKS_DIR / "spec-dense"

# The regular-expression findall most single passes start from: `\d` and
# float() accept digits the README forbids, and nothing checks order, zero or
# where the fraction sits.
HABITUAL_DURATION = '''import re
_U = {"d": 86400, "h": 3600, "m": 60, "s": 1}
def parse_duration(text):
    if not isinstance(text, str):
        raise TypeError("text")
    s = text.strip()
    neg = s.startswith("-")
    parts = re.findall(r"(\\d+(?:\\.\\d+)?)([dhms])", s.lstrip("-"))
    if not parts:
        raise ValueError(text)
    total = int(sum(float(n) * _U[u] for n, u in parts))
    return -total if neg else total
def format_duration(seconds):
    if not isinstance(seconds, int):
        raise TypeError("seconds")
    if seconds == 0:
        return "0s"
    out, rest = "-" if seconds < 0 else "", abs(seconds)
    for u in "dhms":
        n, rest = divmod(rest, _U[u])
        if n:
            out += "%d%s" % (n, u)
    return out
'''


class SpecDenseTest(unittest.TestCase):
    """spec-dense: rule density in one module rather than one surprising rule
    per module."""

    def prepared(self, fixed=False):
        d = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, d)
        task = grade.load_task(DENSE_TASK)
        if fixed:
            subprocess.run([sys.executable, str(task["dir"] / "generate.py"), str(d), "--fixed"], check=True)
        else:
            agent_eval.prepare_workspace(task, d)
        return d

    def hidden(self, ws):
        return subprocess.run([sys.executable, str(DENSE_TASK / "hidden" / "run_hidden.py"), "duration"], cwd=ws,
                              capture_output=True, text=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))

    def test_reference_meets_every_rule(self):
        task = grade.load_task(DENSE_TASK)
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), self.prepared(fixed=True), "",
                                   [], grade.hash_tree(self.prepared()))
        self.assertTrue(all(r["ok"] for r in results), [r for r in results if not r["ok"]])

    def test_workspace_has_no_solution_or_rule_tests(self):
        ws = self.prepared()
        self.assertEqual(sorted(p.name for p in (ws / "duration").iterdir()), ["README.md", "test_basic.py"])

    def test_the_usual_findall_passes_the_visible_tests_but_misses_rules(self):
        ws = self.prepared()
        (ws / "duration" / "duration.py").write_text(HABITUAL_DURATION)
        visible = subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", "duration"], cwd=ws,
                                 capture_output=True, text=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
        self.assertEqual(visible.returncode, 0, visible.stderr)
        failed = self.hidden(ws).stdout.strip().splitlines()[-1].removeprefix("failed rules: ").split(",")
        self.assertGreaterEqual(len(failed), 6, failed)


if __name__ == "__main__":
    unittest.main()
