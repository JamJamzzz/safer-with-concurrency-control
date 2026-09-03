## Concurrency benchmark

Run the SAFER-CC correctness and performance benchmark with:

```bash
go run ./cmd/benchmark
```

The command shows workload progress, correctness status, throughput, p95
latency, and lost-write counts in the terminal. It writes the complete results
to `benchmarks/benchmark-results.json` and `benchmarks/benchmark-report.md`.

Useful options:

```bash
go run ./cmd/benchmark -out ./results
go run ./cmd/benchmark -quiet
go run ./cmd/benchmark -color never
```
