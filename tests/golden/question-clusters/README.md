# Question cluster pair set

106 manually labeled Chinese/English question pairs for embedding threshold evaluation.

- 53 `same=true`: paraphrases or near-duplicates that should share a cluster.
- 53 `same=false`: related but distinct topics that should not share a cluster.
- Newer rows include `annotatorA` / `annotatorB`; older rows default both to `same`.
- `same` is the adjudicated label used for precision/recall/F1.

Coverage includes brand understanding, products, competitors, pricing, deployment, security, integrations, metrics, reports, operations, review, and English paraphrases.

Run `cmd/cluster-eval` with `GEO_EMBEDDING_*` configured to sweep thresholds, report precision/recall/F1 and Cohen's kappa. This is a regression set, not a statistically representative benchmark.
