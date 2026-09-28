# Intelligence monitor behavioral fingerprints

Source: <https://github.com/haowang02/cpa-plugin-codex-candy-eval>

Pinned source commit: `97d73994842d3c236d39be12654d46c2fa66283c` (retrieved 2026-09-28).
The accompanying MIT license, copyright (c) 2026 Hao Wang, applies to the
adapted comparison algorithm and copied `probes.json` / `baselines.json`.

These two JSON files are copied unchanged from the source's
`internal/plugin/data/fingerprint_probes.json` and
`internal/plugin/data/fingerprint_baselines.json`. They contain 16 probe cells
and seven named reference distributions, including `gpt-6-astra`; each
reference has 25 samples per cell except one 24-sample cell. The files total
about 30 KiB. They are community-supplied baselines, not provider attestations.

The monitor uses the source's quick protocol: the first four cells, each
sampled independently 15 times, `temperature=1`, `reasoning.effort=low`, and
the exact embedded instructions/prompts. A minimum of 10 valid observations
in each of at least four cells is required. This is 60 model requests per
collection, separate from the candy question. Reusing the candy answer,
comparing model response headers, or collecting one sample does not implement
this protocol and cannot establish a passing behavioral comparison.

The algorithm compares mean Jensen-Shannon divergence, with 1,000 seeded
permutations and the source's Bonferroni correction (`0.05 / (2 * models)`).
Split-half inconsistency remains a warning or an unstable result. Only
`consistent` is a passing result; lack of a baseline, ambiguity, missing
samples, instability, or a statistically different output is not a pass.

This is a behavioral consistency check, not proof of model identity. No
claim is made that these model identifiers, data provenance, or model
versions have been independently authenticated.
