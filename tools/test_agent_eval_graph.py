"""Offline contracts of the graph eval arm: the graph_count check and the two
graph tasks' fixtures. No model, no network."""
import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402

GRAPH = {
    "subjects": [
        {"kind": "fofa_query", "status": "retracted", "attrs": {"query": 'title="a"'}},
        {"kind": "fofa_query", "status": "confirmed", "attrs": {"query": 'title="b"'}},
        {"kind": "ip", "status": "lead", "attrs": {}},
    ],
    "links": [{"rel": "refines"}, {"rel": "hosts"}],
    "todos": [{"status": "done"}, {"status": "done"}, {"status": "failed"}],
}


def run(check, graph=GRAPH):
    return grade.run_checks([{"type": "graph_count", **check}], Path("."), "", [], {}, graph)[0]


class GraphCountTest(unittest.TestCase):
    def test_filters_narrow_by_kind_status_rel_and_attribute(self):
        self.assertTrue(run({"of": "subjects", "kind": "fofa_query", "min": 2, "max": 2})["ok"])
        self.assertTrue(run({"of": "subjects", "kind": "fofa_query", "status": "retracted", "min": 1, "max": 1})["ok"])
        self.assertTrue(run({"of": "subjects", "status": ["lead", "confirmed"], "max": 2, "min": 2})["ok"])
        self.assertTrue(run({"of": "links", "rel": "refines", "min": 1, "max": 1})["ok"])
        self.assertTrue(run({"of": "todos", "status": "done", "min": 2, "max": 2})["ok"])
        self.assertTrue(run({"of": "subjects", "attrs": {"query": "title=.b"}, "min": 1, "max": 1})["ok"])

    def test_a_bound_that_is_missed_fails_with_the_count(self):
        result = run({"of": "todos", "status": "failed", "max": 0})
        self.assertFalse(result["ok"])
        self.assertIn("count 1", result["detail"])

    def test_a_run_without_a_graph_fails_instead_of_passing_vacuously(self):
        result = run({"of": "todos", "status": "open", "max": 0}, None)
        self.assertFalse(result["ok"])
        self.assertIn("no graph", result["detail"])

    def test_an_unknown_collection_is_refused_when_the_task_loads(self):
        with self.assertRaises(grade.TaskError):
            grade._validate_check("t", {"type": "graph_count", "of": "nodes"})

    def test_graph_checks_nest_in_any_of(self):
        any_of = {"type": "any_of", "checks": [
            {"type": "graph_count", "of": "todos", "status": "open", "min": 1},
            {"type": "graph_count", "of": "todos", "status": "done", "min": 1}]}
        self.assertTrue(grade.run_checks([any_of], Path("."), "", [], {}, GRAPH)[0]["ok"])


def load(path):
    spec = importlib.util.spec_from_file_location(path.stem + "_t", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class GraphTaskFixtureTest(unittest.TestCase):
    def test_both_tasks_load_and_use_the_graph_check(self):
        for name in ("graph-exposure-analysis", "graph-fofa-query"):
            task = grade.load_task(agent_eval.TASKS_DIR / name)
            self.assertTrue(any(c["type"] == "graph_count" for c in task["checks"]), name)

    def test_exposure_answer_key_matches_the_generated_export(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "graph-exposure-analysis")
        rows = load(task["dir"] / "generate.py").records()
        keyed = {c["name"]: c["value"] for c in task["checks"] if "value" in c}
        self.assertEqual(keyed["ip_count"], len({r["ip"] for r in rows}))
        for name, field in (("ips_by_country", "country"), ("ips_by_cloud", "cloud")):
            want = {}
            for r in rows:
                want.setdefault(r[field], set()).add(r["ip"])
            self.assertEqual(keyed[name], {k: len(v) for k, v in want.items()})
        self.assertEqual(keyed["products"], sorted({r["product"] for r in rows}))

    def test_query_grader_rejects_the_draft_and_a_loose_rewrite_and_accepts_the_exact_one(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "graph-fofa-query")
        cases = {
            'title="Aurora"': False,  # the seeded draft: also selects unrelated Aurora sites
            'body="Aurora Gateway"': False,  # selects the blog posts that merely mention the product
            'title="Aurora Gateway - Sign in" || header="aurora-gw/"': True,
        }
        for query, ok in cases.items():
            with tempfile.TemporaryDirectory() as tmp:
                workspace = Path(tmp)
                agent_eval.prepare_workspace(task, workspace)
                (workspace / "query.txt").write_text(query + "\n")
                checks = grade.expand_task_dir(task["checks"], task["dir"])
                result = grade.run_checks(checks[:1], workspace, "", [], grade.hash_tree(workspace))[0]
                self.assertEqual(result["ok"], ok, (query, result))

    def test_a_query_the_simulator_cannot_parse_fails_instead_of_crashing(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "graph-fofa-query")
        with tempfile.TemporaryDirectory() as tmp:
            workspace = Path(tmp)
            agent_eval.prepare_workspace(task, workspace)
            (workspace / "query.txt").write_text('title=Aurora\n')
            checks = grade.expand_task_dir(task["checks"], task["dir"])
            result = grade.run_checks(checks[:1], workspace, "", [], {})[0]
            self.assertFalse(result["ok"])
            self.assertIn("does not run", result["detail"])

    def test_query_grader_reads_the_task_results_copy(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "graph-fofa-query")
        with tempfile.TemporaryDirectory() as tmp:
            workspace = Path(tmp)
            agent_eval.prepare_workspace(task, workspace)
            dest = workspace / ".zwai-tasks" / "th_x" / "results"
            dest.mkdir(parents=True)
            (dest / "query.txt").write_text('title="Aurora Gateway - Sign in" || header="aurora-gw/"\n')
            checks = grade.expand_task_dir(task["checks"], task["dir"])
            result = grade.run_checks(checks[:1], workspace, "", [], grade.hash_tree(workspace))[0]
            self.assertTrue(result["ok"], result)


if __name__ == "__main__":
    unittest.main()
