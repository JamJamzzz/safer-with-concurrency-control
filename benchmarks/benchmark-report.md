# SAFER-CC V1 Phase 6 Benchmark Report

Generated: 2026-08-29T02:28:23Z

## Environment

- OS: windows
- Arch: amd64
- Go version: go1.26.5
- Logical CPUs (runtime.NumCPU): 12
- GOMAXPROCS (default, unmodified): 12
- Repetitions per trial: 5 (median ops/sec reported; min/max also recorded)

## Methodology

Three strategies are compared, all executing the *same* SAFER business logic (crypto validation, datastore reads/writes, chunk traversal, Metadata.Version bookkeeping, authorization checks) -- only the coordination primitive backing each operation's lock guard differs (see client.go, `newOperationGuard`):

- **NoCC**: no logical coordination at all (storage-engine latches `datastoreMu`/`keystoreMu` remain active regardless -- they protect Go map memory safety, not SAFER transactions).
- **GlobalLock**: one process-wide mutex serializes each public operation start-to-finish.
- **SAFER-CC**: the real production fine-grained Namespace/File S/X strict-2PL implementation.

Each workload's correctness oracle is `Metadata.Version` (via the benchmark-only diagnostic export `DebugFileVersionForBenchmark`): for V successful content mutations against a starting version V0, a correct system must show final Version == V0+V. Any shortfall is counted as `lost writes` and is the strongest, most direct evidence of a logical race, independent of whether any operation call itself returned an error.

## Workload A: same-file concurrent append

1 file, 10 ops/worker, worker counts 1/4/16/64, all workers appending to the *same* logical file.

| Strategy | Workers | Ops/s (median) | Ops/s (min-max) | p50 (us) | p95 (us) | Errors | Lost writes | Correct |
| -------- | ---: | -------------: | ---------------: | -------: | -------: | -----: | -----------: | ------- |
| NoCC | 1 | 9964 | 0-19826 | 0 | 504 | 0 | 0 | yes |
| NoCC | 4 | 40036 | 26257-41494 | 0 | 999 | 0 | 83 | NO -- lost writes |
| NoCC | 16 | 53195 | 45623-53959 | 0 | 1006 | 0 | 682 | NO -- lost writes |
| NoCC | 64 | 54059 | 46351-57543 | 994 | 3052 | 0 | 3084 | NO -- lost writes |
| GlobalLock | 1 | 9083 | 8533-10122 | 0 | 573 | 0 | 0 | yes |
| GlobalLock | 4 | 12167 | 11241-13466 | 0 | 1262 | 0 | 0 | yes |
| GlobalLock | 16 | 11096 | 10567-11812 | 1004 | 2484 | 0 | 0 | yes |
| GlobalLock | 64 | 11475 | 11193-11717 | 5397 | 6536 | 0 | 0 | yes |
| SAFER-CC | 1 | 10004 | 0-10327 | 0 | 968 | 0 | 0 | yes |
| SAFER-CC | 4 | 9873 | 9452-13151 | 0 | 1504 | 0 | 0 | yes |
| SAFER-CC | 16 | 10592 | 10569-11029 | 1043 | 2083 | 0 | 0 | yes |
| SAFER-CC | 64 | 10300 | 9814-10409 | 6108 | 7363 | 0 | 0 | yes |

## Workload B: same-file read-heavy (90% LoadFile / 10% AppendToFile)

1 file (pre-seeded with 20 chunks so LoadFile does real chunk traversal), 20 ops/worker, worker counts 1/4/16/64.

| Strategy | Workers | Ops/s (median) | Ops/s (min-max) | p50 (us) | p95 (us) | Errors | Lost writes | Correct |
| -------- | ---: | -------------: | ---------------: | -------: | -------: | -----: | -----------: | ------- |
| NoCC | 1 | 4424 | 3999-5667 | 0 | 1006 | 0 | 0 | yes |
| NoCC | 4 | 10717 | 8199-12338 | 0 | 1434 | 0 | 1 | NO -- lost writes |
| NoCC | 16 | 11992 | 9546-15979 | 999 | 2504 | 0 | 31 | NO -- lost writes |
| NoCC | 64 | 4432 | 4189-4748 | 11193 | 29332 | 0 | 197 | NO -- lost writes |
| GlobalLock | 1 | 4826 | 3600-5034 | 0 | 1003 | 0 | 0 | yes |
| GlobalLock | 4 | 3455 | 3244-3493 | 1000 | 2643 | 0 | 0 | yes |
| GlobalLock | 16 | 2435 | 2234-2531 | 6083 | 10003 | 0 | 0 | yes |
| GlobalLock | 64 | 1061 | 1056-1096 | 57826 | 100003 | 0 | 0 | yes |
| SAFER-CC | 1 | 3999 | 3312-4523 | 0 | 1008 | 0 | 0 | yes |
| SAFER-CC | 4 | 8380 | 7560-9396 | 0 | 1263 | 0 | 0 | yes |
| SAFER-CC | 16 | 7177 | 6453-7509 | 1999 | 4545 | 0 | 0 | yes |
| SAFER-CC | 64 | 2778 | 2736-2783 | 22716 | 38694 | 0 | 0 | yes |

## Workload C: independent files

32 logical files, workers round-robin assigned to files by `worker_index % 32`, alternating Append/Load. At 16 workers every worker owns a distinct file (no two workers ever touch the same file). At 64 workers, 32 files means exactly 2 workers per file, so that row is a deliberate partial-overlap case, not a pure independent-files case -- this is why NoCC shows lost writes at 64 workers (2 real writers per file) but not at 16 (1 writer per file, no collision possible). The performance comparison below still holds: this is exactly the same file-assignment pattern for every strategy, so it is an apples-to-apples comparison either way.

| Strategy | Workers | Ops/s (median) | Ops/s (min-max) | p50 (us) | p95 (us) | Errors | Lost writes | Correct |
| -------- | ---: | -------------: | ---------------: | -------: | -------: | -----: | -----------: | ------- |
| NoCC | 16 | 53287 | 50400-53943 | 0 | 0 | 0 | 0 | yes |
| NoCC | 64 | 55744 | 47952-55774 | 0 | 0 | 0 | 713 | NO -- lost writes |
| GlobalLock | 16 | 9809 | 9323-9993 | 0 | 0 | 0 | 0 | yes |
| GlobalLock | 64 | 8772 | 8532-8870 | 0 | 0 | 0 | 0 | yes |
| SAFER-CC | 16 | 53515 | 39400-81400 | 0 | 0 | 0 | 0 | yes |
| SAFER-CC | 64 | 49337 | 41269-50002 | 0 | 0 | 0 | 0 | yes |

## Workload D: reader concurrency

1 shared file (20 pre-seeded chunks), 64 concurrent readers, 20 LoadFile calls each, no writers.

| Strategy | Workers | Ops/s (median) | Ops/s (min-max) | p50 (us) | p95 (us) | Errors | Lost writes | Correct |
| -------- | ---: | -------------: | ---------------: | -------: | -------: | -----: | -----------: | ------- |
| NoCC | 64 | 9526 | 9433-9949 | 2997 | 12257 | 0 | 0 | yes |
| GlobalLock | 64 | 3332 | 3327-3376 | 19131 | 20986 | 0 | 0 | yes |
| SAFER-CC | 64 | 8988 | 8850-9387 | 3425 | 14154 | 0 | 0 | yes |

## Workload E: authorization contention (correctness-focused)

1 owner, 8 recipients. Owner concurrently issues 8 invitations; first 4 recipients concurrently Accept while the owner concurrently Revokes them (forced Accept-vs-Revoke race, no forced winner); last 4 recipients Accept then Load+Append with no concurrent revoke. Single trial per strategy (this workload evaluates correctness, not throughput). After the workload, `CheckAuthorizationInvariantsForBenchmark` re-validates persistent authoritative state.

| Strategy | Attempts | Expected rejects | Unexpected errors | Invariant violation |
| -------- | -------: | ----------------: | -----------------: | -------------------- |
| NoCC | 28 | 4 | 0 | none |
| GlobalLock | 28 | 1 | 0 | none |
| SAFER-CC | 28 | 4 | 0 | none |

## Correctness findings

- **NoCC reproduces real lost updates**: across Workload A's repetitions, the NoCC strategy lost 3849 successful append(s) that Metadata.Version failed to reflect -- concrete, measured evidence that the pre-SAFER-CC design's lack of logical coordination causes silent data loss under concurrent same-file writers, exactly as the Phase 1 audit predicted.
- GlobalLock lost writes across Workload A: 0. SAFER-CC lost writes across Workload A: 0. Both are expected to be zero -- both strategies fully serialize writers to the same file.
- Workload C: GlobalLock lost writes: 0. SAFER-CC lost writes: 0. Both must be (and are) zero even at 64 workers/32 files, where 2 workers share each file -- SAFER-CC's per-FileID File X correctly serializes those same-file pairs exactly as it does in Workload A, while still letting the other 31 files' operations run fully in parallel (see the throughput comparison below).
- Workload E (authorization contention) under **SAFER-CC**: zero unexpected errors and zero persistent authorization-invariant violations -- every rejection observed was a designed semantic outcome (e.g. Accept losing a race to Revoke), not concurrency corruption.


## Performance findings

- **Workload A (same-file append, 64 workers)**: GlobalLock 11475 ops/s (median) vs SAFER-CC 10300 ops/s (median). Both strategies serialize all writers to the one shared file, so SAFER-CC is not expected to (and should not be claimed to) meaningfully outperform a global lock here -- any difference reflects fine-grained-locking bookkeeping overhead (lock-manager queue/map operations) rather than added parallelism.
- **Workload C (independent files, 64 workers / 32 files)**: SAFER-CC achieved 5.6x GlobalLock's median throughput (49337 vs 8772 ops/s) -- this is the workload where per-file logical resource granularity is expected to (and, per this measurement, does) enable real parallelism that a single global mutex structurally cannot.
- **Workload D (64 concurrent readers, one shared file)**: SAFER-CC achieved 2.7x GlobalLock's median throughput (8988 vs 3332 ops/s) -- evidence that Shared-mode File locks let genuinely concurrent readers overlap, where a global mutex forces them one-at-a-time regardless of the fact that none of them conflict.
- **Workload B (90% read / 10% write, one shared file, 64 workers)**: SAFER-CC achieved 2.6x GlobalLock's median throughput (2778 vs 1061 ops/s). This workload mixes S and X requests on one file, so the advantage (if any) is smaller than Workload D's pure-reader case: X requests from the 10% writer share still serialize against all readers and other writers under both strategies.


## Limitations

- Concurrency levels for Workloads A/B were run at 1/4/16/64 workers (not the full 1..128 sweep) to keep total benchmark runtime reasonable; this is a scope reduction explicitly disclosed, not a hidden one.
- Workload E's expected-vs-unexpected error classification is a substring heuristic over known SAFER error messages (`isExpectedAuthError` in cmd/benchmark/main.go), not a formal proof that every rejection is semantically correct -- it is, however, cross-checked against the persistent authorization-invariant checker, which is exact.
- All measurements were taken on one shared development machine under normal OS scheduling, not an isolated/pinned benchmarking environment; absolute ops/sec numbers should be read as order-of-magnitude and relative-comparison evidence, not precise throughput ceilings.
- The Go race detector could not be run in this environment (no C toolchain); these results are not `-race`-verified. See docs/concurrency-control.md and review.md for details.
