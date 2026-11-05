# Nuwa development observations, 2026-11-05

These observations have not established the strict joint objective: higher correctness, shorter foreground time and fewer all-attempt reported tokens in the same matched pair, with actual collected heterogeneous material work. Quality takes priority. A faster incomplete answer does not satisfy that objective.

A is ordinary product mode with `glm-5.3-flash` as its initial model. B enables Nuwa with the same configured catalog, including `dsv41-flash-1017` for selectable workers. Managers choose whether to delegate and select their own scopes. Ordinary mode may also create GLM workers. A product-mode comparison only supports a heterogeneous mechanism interpretation when actual DS requests, generation completion and collection are observed. Collection alone does not prove independent derivation or material use.

## Larger-budget matched results

The following pairs offered a 131,072-token completion cap per request and 1,800 seconds of foreground headroom to both arms. The cap is an accepted request setting, not a guarantee that the upstream provider can generate that many tokens. Other limits, source versions and task histories differ between pairs; these results must not be pooled as one benchmark.

| Task and evidence record | Exact quality A → B | Foreground seconds A → B | Reported tokens A → B | Actual heterogeneous work |
|---|---:|---:|---:|---|
| Reserved high logistics, `logistics-headroom-03` | 5/5 → 5/5 | 525.834 → 320.732 | 1,692,524 → 1,481,673 | B had 16 DS requests and one generation with a recorded finish and collection. Independent source partition and material-use credit are not established by this summary. |
| Drone planning/control, `drone-idle240-02` | 0/30 → 8/30 | 561.045 → 141.290 | 1,973,459 → 483,845 | No DS requests in either arm. A created one GLM worker; B created none. |
| Workbook recovery, `xlsx-idle240-01` | 18/19 → 18/19 pytest cases; official binary score 0 → 0 | 37.212 → 56.398 | 120,416 → 242,491 | Neither arm created a worker. |

The Drone and workbook foreground values come from the raw completion records. Their paired summaries separately retain accepted-to-durable windows of 561.419 → 140.909 seconds and 37.199 → 56.388 seconds; those windows are not substituted for foreground time here. Both experiments used the engine built from `ed823c9abb5fa32008738ad5df4e19fcb434f730`, ELF SHA-256 `c5008edcb25b137e67056e461f9bce2acbbe24d2e72548a1dfb0930a9328fb9b`. These observations do not validate a later product build.

The logistics pair is a descriptive quality-preserving efficiency observation: B took 39.00% less foreground time and reported 12.46% fewer tokens, while both achieved the same exact grade. The 5/5 rubric contains five groups over three packet outputs, 36 dispositions with exact blocker roots, and five counts. It does not mean five separate tasks. This packet has 46 active dependency edges and depth 17. Exact grades show that both runs solved this case; one pair at the score ceiling cannot estimate a correctness advantage or generalization. The request audit reports three A and two B joins using its disclosed wider timing diagnostic. Those descriptive totals must not silently replace a stricter frozen admission result.

Drone B improved the original judge score by eight commands and used less time and fewer tokens. Both arms still failed the full task. All 30 command deliveries passed the separate output/custody check, so delivery success must not be confused with controller correctness. With no DS use, this pair provides no evidence for a GLM-manager/DS-executor benefit. The original 95-second-idle attempts remain infrastructure failures. The successor increased both idle bounds to 240 seconds, retained the 120-second engine first-byte bound and other matched budgets, and was admitted by an actual local SSE control with a 125.005-second mid-stream quiet gap. The relay open/header wait uses the same increased bound. This was an explicit transport sensitivity successor, not an overwrite of the earlier failures.

Workbook B was 51.56% slower and used 101.38% more tokens under the unchanged official judge. Both arms failed `test_growth_values`. A separate offline diagnostic kept both submitted workbooks unchanged and changed only the expected `Growth Analysis!B8` value from 7,444.4 to 7,610.333333333333. The latter is the six-year mean derived from the public values; untouched public columns support the same averaging rule. Both workbooks then passed all 19 pytest cases. This supports an expected-cell inconsistency in that check. The diagnostic used a macOS Python process with a Python audit hook denying socket operations, not the official Linux runtime, and does not alter either official zero or create a correctness difference between arms.

## Observatory remains a separate 65,536-cap observation

The completed shared-scratch Observatory pair used a 65,536-token request cap, 1,200-second foreground budget and 95-second idle setting. It is not part of the 131,072-cap table above.

| Exact rows A → B | Foreground seconds A → B | All-attempt reported tokens A → B | Actual DS engagement in B |
|---:|---:|---:|---|
| 44/48 → 48/48 | 82.640 → 85.459 | 660,209 → 579,023 | 19 DS requests across three collected generations |

B improved exact quality and reduced token volume by 12.30%, while taking 3.41% longer. Public source bytes were unchanged in both arms. A had one false branch pass; B had zero false passes or ready decisions. The strict typed-write ownership metric recorded no final DS output writes in this pair, so DS collection cannot be promoted to exact-output ownership by inference from shell text. The shared prompt included accumulated guidance about bounded evidence and scratch ownership, preventing an isolated causal attribution to one instruction.

The short autonomous successor has only been prepared offline at the time of this note. It freezes a shorter common prompt, BA order, the original 48-row public task and scorer, the stock `ed823c9a` engine, 131,072/1,800 headroom and 240/120-second timeout settings. It has no real-model result in this record. A scripted local transport control is not a model-quality observation.

## Measurement and acceptance boundaries

- Foreground time follows each run's recorded terminal window. It excludes engine startup, fixture preparation, later grading and shutdown. It is not complete end-to-end cost.
- Reported token volume includes admitted attempts, including captured auxiliary/post-terminal work where the ledger is complete. Provider-reported cached tokens are retained separately. Token counts across models are not money or equal compute.
- A provider error, output-limit cutoff, request ceiling, unknown usage or incomplete settlement stays in the assigned denominator with its recorded classification. It cannot become a normal incorrect answer or disappear through a replacement run. The separate fresh-projection logistics attempt has A exact 5/5 with one unknown post-terminal usage record and B abnormal 0/5; it does not qualify for a complete paired efficiency comparison.
- Each entry is a small development observation. Exposure history, order, source version and provider load prevent a broad causal or statistical conclusion. Shared prompts are equal within a pair, not equal across all experiments.
- Equal full grades can support quality-preserving efficiency on the observed case. Strict correctness improvement requires a graded difference; strict joint success additionally requires full quality, better time and tokens, complete measurement, and actual collected DS material use. No row above establishes all of these together.

## Evidence identities

Private raw evidence is retained outside the repository. The following SHA-256 identities permit the evidence owner to recover the exact records without publishing routes, credentials, raw prompts or local machine details.

| Record | SHA-256 |
|---|---|
| `logistics-headroom-03` solver completion | `3a6c97b0b9fe6278f2a9820cbc8545dd98066137de4843a925b2b0ed1157a39a` |
| `logistics-headroom-03` descriptive request audit | `c8de8ddc95193cc90f9be7c927601eb5ee66c2e0b56480b6e033eb65ae2d1207` |
| `drone-idle240-02` raw solver completion | `bec5a11abde993c79c55addb836e05fcdac0fbbb77e80b347f1a37271764b9a8` |
| `drone-idle240-02` paired result | `41a731b0e69866bbc1b88806202a8309a63d38a86cafa22ed3fe5d01833e4f8f` |
| `xlsx-idle240-01` raw solver completion | `07140773f9448432378d13183348208742d0dd927acc4350c8b126c9bd0d38dd` |
| `xlsx-idle240-01` paired result | `6acf161128e2dabb85599f6bbdf1bcdcc881fb414011344bbf3ac55a00b380a8` |
| Workbook one-cell counterfactual diagnostic | `a65f0f7c274b23d7984a400364accad271bd06947cc017574ba974b15fdaef07` |
| Observatory shared-scratch solver completion | `928eb3a5d6bbd4eb34ee41ba32d0360753d0911001e4d33ff720898dd6b797d0` |
| Observatory independent grade | `f45b68d5e99b226a7d984fe085da6df6d64fe7b4b3abc8dc6fe8c0899f4e5f87` |
| Observatory protocol | `e6e4cc384cbc1df4fa54d8cb430a92953c6a4d078cc4e108ff11743522f100e0` |

This is a dated addition to the historical [Nuwa results](RESULTS.md); earlier records retain their original scope.
