# Project-history task measurement

`raw.json` contains two independent synthetic projects and ordinary Chinese
requests. The asset case excludes writes; the document case reverses an older
read-only decision to allow one explicitly submitted, idempotent write. Both
include unrelated-project interference, older/newer decisions, unverified
suggestions and negative boundaries. Dates and source text are frozen.

`expectations.json` is an independently authored semantic review rubric. The
harness never writes it, the raw fixture or an answer key into the model's
workspace. It seeds only raw synthetic messages, turns and events into a fresh
private database after creating projects/conversations over the real HTTP API.
The engine is stopped during seeding and restarted with fresh sequence counters.

Run the real-model measurement explicitly, using an original binary built before
the feature and an identified candidate. Each case receives the same natural
request in both arms. Baseline runs once per case; `--runs` controls candidate
repetitions. Neither prompt tells the model what SQL to execute.

```bash
python3 tools/project_history_eval.py \
  --provider <configured-provider-id> \
  --baseline-zwai /absolute/original/zwai \
  --zwai /absolute/candidate/zwai \
  --runs 2 --out /absolute/private/evidence

python3 -m unittest tools/test_project_history_eval.py
python3 tools/project_history_eval.py --mock --zwai /absolute/candidate/zwai
```

`--reasoning-effort low|medium|high` applies an existing UI thinking option only
to each fresh private target conversation, using PATCH followed by GET readback
before the ordinary task request. Omit it to retain the engine's default; it
does not change the user's global configuration or model. The report records
the requested/read-back effort and checks the Trace turn. Any recorded model-call
effort must match for manager/worker calls; auxiliary policy calls keep their own
setting. A missing model-call effort field is reported as unavailable, never
invented from reasoning-token counts. Mismatched readback or Trace is a harness
failure. Even successful HTTP requests and additional reasoning tokens do not
prove that the upstream service adopted the setting or increased computation.
Describe such a run as the requested-effort condition, and preserve the separate
default-effort observations.

Only the selected provider is copied from config; actual user conversations are
never read. Every run owns its private engine process, loopback port and data
directory. Explicitly titled conversations prevent unrelated automatic naming.
HOME, XDG default paths, ZDOTDIR and ZWAI_HOME are private for each child. This
changes default discovery paths; it does not confine the product's full-access
tools. Review actual tool-call paths before accepting a measurement.
Evidence files use `0600`; the harness leaves the subprocess umask unchanged.
The private provider configuration is removed after the engine exits. API keys
are redacted before evidence is saved. `--mock` checks infrastructure only and
cannot establish real-model effectiveness.

Evidence retains the complete answer, Trace, SQL chosen by actual manager or
worker model calls, original source mapping, binary/fixture hashes, failures,
timing and cleanup result. Successful SQL retrieval and complete-source coverage
are structural observations. They do not prove that the answer preserves a
negation, resolves conflicting decisions correctly, cites the right source or
distinguishes a suggestion from an accepted capability. SQL substring paging may
provide valid evidence without satisfying the conservative full-record coverage
counter; inspect its Trace rather than treating it as a semantic failure.
Cancellation and exceptions collect the current raw Trace before stopping the
engine and deleting its private data. An evidence failure is reported separately
and never replaces the original error. New rounds get new evidence directories.

The foreign-project canary check applies to every actual tool result, including
an original binary's `exec` path, and to write/edit arguments as well as the final
answer. An original binary may expose all history through its general tools. The current
candidate's project-bound SQL view must exclude foreign records even without a
WHERE clause, while standalone conversations retain global history access. The
canary also checks full-access tool behavior for this current-project task; the
SQL binding does not confine those system tools.

Audit each `semantic-review.json` against `answer.md`, `raw.json`,
`source-map.json` and `traces.json`. Record a verdict and the answer passage for
every rubric item, including baseline and failed runs. Reports deliberately stay
`semantic_status: unreviewed` until this independent review is completed. No
automatic pass rate or efficacy claim is produced by the harness.

The harness sends an ordinary request without `history_report`, so the product
must select history retrieval itself and answers in ordinary prose: reading
history never switches that turn into the source-report protocol. Do not
inject the report schema, SQL or expected facts through the harness. A
source-report measurement needs the explicit start-turn flag, which this
harness does not send; label such a run separately rather than comparing it
with ordinary-request rounds as the same condition. For a report answer, review
model-selected references and each proposal/title/priority separately
from the host's full raw evidence section. An attributed quote of an older
assistant claim is not a new assertion that the claim is verified, but repeating
it as a current premise in a proposal still needs support. Full raw-record
coverage and a mechanically valid report are not semantic acceptance. Preserve
prior default/requested-effort failures when a later candidate is measured;
acceptance belongs to the exact frozen binary and condition, never the feature
name alone.
