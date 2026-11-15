# Question cluster pair set

30 manually labeled Chinese question pairs for embedding threshold evaluation.

- `same=true`: paraphrases or near-duplicates that should share a cluster.
- `same=false`: related but distinct topics that should not share a cluster.

Run `cmd/cluster-eval` with `GEO_EMBEDDING_*` configured to sweep thresholds and report precision/recall/F1. This is a small regression set, not a statistically representative benchmark.
