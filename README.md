# Project 2 Starter Code

This repository contains the starter code for Project 2!

For comprehensive documentation, see the Project 2 Spec (https://cs161.org/proj2/).

A friendly request: please do not make your solution public!

Write your implementation in `client/client.go` and your integration tests in `client_test/client_test.go`. Optionally, you can also use `client/client_unittest.go` to write unit tests (e.g: to test your helper functions).

To run all packages, including the white-box, black-box, lock-manager, and
benchmark UI tests, run `go test -count=1 ./...` from the repository root.
Running only inside `client_test` does not run the other packages' tests.

## CI and concurrency verification

[CI workflow](.github/workflows/ci.yml) runs on pull requests and pushes to
`main`, with read-only repository permissions. Two independent GitHub-hosted
`ubuntu-latest` jobs run the complete suite:

```bash
go test -mod=readonly -count=1 -v -timeout=10m ./...
CGO_ENABLED=1 CC=gcc go test -race -mod=readonly -count=1 -v -timeout=15m ./...
```

Go is selected from `go.mod` (`go 1.20`, no separate `toolchain` directive),
using the latest patch of that release series. No module-version change is
part of this CI work. The normal job disables cgo; this project does not
require it. The race job verifies Go, `CGO_ENABLED=1`, GCC, and the package
list before testing. `-count=1` disables test-result caching and
`-mod=readonly` prevents implicit dependency changes. The limits bound hung
tests; they are not performance thresholds.

The original Windows environment lacked a C compiler and had cgo disabled.
Linux CI provides that toolchain; an actual successful hosted run is still
required before claiming race-detector verification. See
[the evidence/status section](review.md#8-race-detector-evidencestatus).
To reproduce the race command locally, use Linux with Go and GCC (or another
Go-supported race-detector platform with its required C toolchain).

The race detector checks memory races in executed paths. The existing
forced-interleaving, strict-2PL, lost-update, and authorization-invariant
tests check logical outcomes. These are complementary: neither proves all
schedules correct or establishes system-wide serializability by itself.
Locks are in-process; crash recovery and cross-process coordination are
outside V1's scope. Benchmarks remain a separate manual tool, not CI
throughput/latency gates or GitHub-runner performance evidence.

## Project Members

Fill in this section with the student IDs of all the members in your project group.

Partner 1 Name:
Haochen Duan
Partner 1 SID:
3041737416
Partner 1 Email:
haochen_duan@berkeley.edu
Partner 2 Name (if applicable):

Partner 2 SID (if applicable):

Partner 2 Email (if applicable):

Also add a link to this repo below (should start with https://github.com/cs161-students/).

Link to this Github repo:
https://github.com/JamJamzzz/safer-with-concurrency-control

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
