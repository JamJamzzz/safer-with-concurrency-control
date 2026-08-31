// Command benchmark runs SAFER-CC's Phase 6 evaluation: three
// concurrency-coordination strategies (No-CC, Global-lock, SAFER-CC) driven
// through the exact same public SAFER API and business logic, across five
// workloads, to produce measured (not fabricated) correctness and
// performance evidence.
//
// Usage: go run ./cmd/benchmark [-out DIR]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	client "github.com/cs161-staff/project2-starter-code/client"
	userlib "github.com/cs161-staff/project2-userlib"
	"github.com/google/uuid"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// ---------------------------------------------------------------------
// Strategies
// ---------------------------------------------------------------------

type strategyDef struct {
	Name  string
	Value client.ConcurrencyStrategy
}

var strategies = []strategyDef{
	{"NoCC", client.StrategyNoCC},
	{"GlobalLock", client.StrategyGlobalLock},
	{"SAFER-CC", client.StrategySaferCC},
}

// ---------------------------------------------------------------------
// Shared result types
// ---------------------------------------------------------------------

type repResult struct {
	ops             int
	errorsCount     int
	elapsed         time.Duration
	latencies       []time.Duration
	lostWrites      int
	versionOK       bool
	finalVersion    uint64
	expectedVersion uint64
}

type trialSummary struct {
	Strategy         string  `json:"strategy"`
	Workers          int     `json:"workers"`
	ExtraDim         int     `json:"extra_dim,omitempty"`
	ExtraDimLabel    string  `json:"extra_dim_label,omitempty"`
	Repetitions      int     `json:"repetitions"`
	OpsAttempted     int     `json:"ops_attempted"`
	MedianOpsPerSec  float64 `json:"median_ops_per_sec"`
	MinOpsPerSec     float64 `json:"min_ops_per_sec"`
	MaxOpsPerSec     float64 `json:"max_ops_per_sec"`
	P50LatencyMicros float64 `json:"p50_latency_micros"`
	P95LatencyMicros float64 `json:"p95_latency_micros"`
	TotalErrors      int     `json:"total_errors"`
	TotalLostWrites  int     `json:"total_lost_writes"`
	AllRepsVersionOK bool    `json:"all_reps_version_ok"`
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func summarize(strategy string, workers, extraDim int, extraDimLabel string, reps []repResult) trialSummary {
	opsPerSecs := make([]float64, len(reps))
	var allLat []time.Duration
	totalErrors, totalLost, opsAttempted := 0, 0, 0
	allOK := true
	for i, r := range reps {
		successOps := r.ops - r.errorsCount
		if r.elapsed > 0 {
			opsPerSecs[i] = float64(successOps) / r.elapsed.Seconds()
		}
		allLat = append(allLat, r.latencies...)
		totalErrors += r.errorsCount
		totalLost += r.lostWrites
		opsAttempted += r.ops
		if !r.versionOK {
			allOK = false
		}
	}
	sort.Float64s(opsPerSecs)
	sort.Slice(allLat, func(i, j int) bool { return allLat[i] < allLat[j] })

	median := 0.0
	if len(opsPerSecs) > 0 {
		median = opsPerSecs[len(opsPerSecs)/2]
	}
	min, max := 0.0, 0.0
	if len(opsPerSecs) > 0 {
		min, max = opsPerSecs[0], opsPerSecs[len(opsPerSecs)-1]
	}

	return trialSummary{
		Strategy:         strategy,
		Workers:          workers,
		ExtraDim:         extraDim,
		ExtraDimLabel:    extraDimLabel,
		Repetitions:      len(reps),
		OpsAttempted:     opsAttempted,
		MedianOpsPerSec:  median,
		MinOpsPerSec:     min,
		MaxOpsPerSec:     max,
		P50LatencyMicros: float64(percentile(allLat, 0.50).Microseconds()),
		P95LatencyMicros: float64(percentile(allLat, 0.95).Microseconds()),
		TotalErrors:      totalErrors,
		TotalLostWrites:  totalLost,
		AllRepsVersionOK: allOK,
	}
}

// ---------------------------------------------------------------------
// Workload A: same-file concurrent append
// ---------------------------------------------------------------------

func workloadAppend(strategy client.ConcurrencyStrategy, workers, opsPerWorker int) repResult {
	userlib.DatastoreClear()
	userlib.KeystoreClear()
	client.SetConcurrencyStrategyForBenchmark(strategy)

	alice, err := client.InitUser("alice", "password")
	must(err)
	must(alice.StoreFile("file1.txt", []byte("base")))

	initialVersion, _, err := client.DebugFileVersionForBenchmark(alice, "file1.txt")
	must(err)

	var wg sync.WaitGroup
	var successCount, errorCount int64
	var latMu sync.Mutex
	var allLatencies []time.Duration

	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := make([]time.Duration, 0, opsPerWorker)
			for i := 0; i < opsPerWorker; i++ {
				payload := fmt.Sprintf("<%d-%d>", w, i)
				t0 := time.Now()
				err := alice.AppendToFile("file1.txt", []byte(payload))
				local = append(local, time.Since(t0))
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
			latMu.Lock()
			allLatencies = append(allLatencies, local...)
			latMu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	finalVersion, _, err := client.DebugFileVersionForBenchmark(alice, "file1.txt")
	must(err)

	expectedVersion := initialVersion + uint64(successCount)
	lostWrites := 0
	if finalVersion < expectedVersion {
		lostWrites = int(expectedVersion - finalVersion)
	}

	return repResult{
		ops: workers * opsPerWorker, errorsCount: int(errorCount), elapsed: elapsed,
		latencies: allLatencies, lostWrites: lostWrites,
		versionOK: finalVersion == expectedVersion, finalVersion: finalVersion, expectedVersion: expectedVersion,
	}
}

// ---------------------------------------------------------------------
// Workload B: same-file read-heavy (90% LoadFile / 10% AppendToFile)
// ---------------------------------------------------------------------

func workloadReadHeavy(strategy client.ConcurrencyStrategy, workers, opsPerWorker int) repResult {
	userlib.DatastoreClear()
	userlib.KeystoreClear()
	client.SetConcurrencyStrategyForBenchmark(strategy)

	alice, err := client.InitUser("alice", "password")
	must(err)
	must(alice.StoreFile("file1.txt", []byte("seed")))
	for i := 0; i < 20; i++ {
		must(alice.AppendToFile("file1.txt", []byte(fmt.Sprintf("-seed%d", i))))
	}
	initialVersion, _, err := client.DebugFileVersionForBenchmark(alice, "file1.txt")
	must(err)

	var wg sync.WaitGroup
	var successAppends, errorCount int64
	var latMu sync.Mutex
	var allLatencies []time.Duration

	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w)*104729 + 7))
			local := make([]time.Duration, 0, opsPerWorker)
			for i := 0; i < opsPerWorker; i++ {
				t0 := time.Now()
				var err error
				if rng.Intn(10) == 0 { // 10% writes
					err = alice.AppendToFile("file1.txt", []byte(fmt.Sprintf("<%d-%d>", w, i)))
					if err == nil {
						atomic.AddInt64(&successAppends, 1)
					}
				} else {
					_, err = alice.LoadFile("file1.txt")
				}
				local = append(local, time.Since(t0))
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
				}
			}
			latMu.Lock()
			allLatencies = append(allLatencies, local...)
			latMu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	finalVersion, _, err := client.DebugFileVersionForBenchmark(alice, "file1.txt")
	must(err)
	expectedVersion := initialVersion + uint64(successAppends)
	lostWrites := 0
	if finalVersion < expectedVersion {
		lostWrites = int(expectedVersion - finalVersion)
	}

	return repResult{
		ops: workers * opsPerWorker, errorsCount: int(errorCount), elapsed: elapsed,
		latencies: allLatencies, lostWrites: lostWrites,
		versionOK: finalVersion == expectedVersion, finalVersion: finalVersion, expectedVersion: expectedVersion,
	}
}

// ---------------------------------------------------------------------
// Workload C: independent files
// ---------------------------------------------------------------------

func workloadIndependentFiles(strategy client.ConcurrencyStrategy, workers, numFiles, opsPerWorker int) repResult {
	userlib.DatastoreClear()
	userlib.KeystoreClear()
	client.SetConcurrencyStrategyForBenchmark(strategy)

	alice, err := client.InitUser("alice", "password")
	must(err)
	filenames := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		filenames[i] = fmt.Sprintf("file-%d.txt", i)
		must(alice.StoreFile(filenames[i], []byte("base")))
	}

	successAppendsPerFile := make([]int64, numFiles)
	var errorCount int64

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			fileIdx := w % numFiles
			fname := filenames[fileIdx]
			for i := 0; i < opsPerWorker; i++ {
				var err error
				if i%2 == 0 {
					err = alice.AppendToFile(fname, []byte(fmt.Sprintf("<%d-%d>", w, i)))
					if err == nil {
						atomic.AddInt64(&successAppendsPerFile[fileIdx], 1)
					}
				} else {
					_, err = alice.LoadFile(fname)
				}
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	lostWrites := 0
	versionOK := true
	for i := 0; i < numFiles; i++ {
		expected := uint64(1) + uint64(successAppendsPerFile[i])
		final, _, err := client.DebugFileVersionForBenchmark(alice, filenames[i])
		must(err)
		if final < expected {
			lostWrites += int(expected - final)
		}
		if final != expected {
			versionOK = false
		}
	}

	return repResult{
		ops: workers * opsPerWorker, errorsCount: int(errorCount), elapsed: elapsed,
		lostWrites: lostWrites, versionOK: versionOK,
	}
}

// ---------------------------------------------------------------------
// Workload D: reader concurrency (pure LoadFile, one shared file)
// ---------------------------------------------------------------------

func workloadReaders(strategy client.ConcurrencyStrategy, readers, opsPerReader int) repResult {
	userlib.DatastoreClear()
	userlib.KeystoreClear()
	client.SetConcurrencyStrategyForBenchmark(strategy)

	alice, err := client.InitUser("alice", "password")
	must(err)
	must(alice.StoreFile("file1.txt", []byte("seed")))
	for i := 0; i < 20; i++ {
		must(alice.AppendToFile("file1.txt", []byte(fmt.Sprintf("-c%d", i))))
	}

	var errorCount int64
	var latMu sync.Mutex
	var allLatencies []time.Duration

	start := time.Now()
	var wg sync.WaitGroup
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, opsPerReader)
			for i := 0; i < opsPerReader; i++ {
				t0 := time.Now()
				_, err := alice.LoadFile("file1.txt")
				local = append(local, time.Since(t0))
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
				}
			}
			latMu.Lock()
			allLatencies = append(allLatencies, local...)
			latMu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	return repResult{
		ops: readers * opsPerReader, errorsCount: int(errorCount), elapsed: elapsed,
		latencies: allLatencies, versionOK: true,
	}
}

// ---------------------------------------------------------------------
// Workload E: authorization contention (correctness-focused)
// ---------------------------------------------------------------------

type authResult struct {
	Strategy           string `json:"strategy"`
	Attempts           int    `json:"attempts"`
	ExpectedRejects    int    `json:"expected_rejects"`
	UnexpectedErrors   int    `json:"unexpected_errors"`
	InvariantViolation string `json:"invariant_violation"`
}

var expectedAuthErrorSubstrings = []string{
	"already in use",
	"not a direct share",
	"recipient does not exist",
	"invalid invitation",
	"invitation authentication failed",
	"invitation and AccessBox FileID mismatch",
	"stale access box",
	"file status",
	"access box",
	"required datastore object is missing",
	"datastore object authentication failed",
	"only the file owner can revoke access",
	"invalid",
	"mismatch",
}

func isExpectedAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range expectedAuthErrorSubstrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func workloadAuthContention(strategy client.ConcurrencyStrategy) authResult {
	userlib.DatastoreClear()
	userlib.KeystoreClear()
	client.SetConcurrencyStrategyForBenchmark(strategy)

	alice, err := client.InitUser("alice", "password")
	must(err)
	must(alice.StoreFile("file1.txt", []byte("shared-content")))

	const numRecipients = 8
	recipients := make([]*client.User, numRecipients)
	names := make([]string, numRecipients)
	for i := 0; i < numRecipients; i++ {
		name := fmt.Sprintf("user%d", i)
		names[i] = name
		u, err := client.InitUser(name, "password")
		must(err)
		recipients[i] = u
	}

	var attempts, unexpected, expectedRejects int64
	record := func(err error) {
		atomic.AddInt64(&attempts, 1)
		if err != nil {
			if isExpectedAuthError(err) {
				atomic.AddInt64(&expectedRejects, 1)
			} else {
				atomic.AddInt64(&unexpected, 1)
			}
		}
	}

	var wg sync.WaitGroup
	invites := make([]uuid.UUID, numRecipients)
	var inviteMu sync.Mutex

	for i := 0; i < numRecipients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			invite, err := alice.CreateInvitation("file1.txt", names[i])
			record(err)
			if err == nil {
				inviteMu.Lock()
				invites[i] = invite
				inviteMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	var successfulAppends int64

	// First half: accepted concurrently with being revoked (Accept vs
	// Revoke race). Second half: accepted, then Load/Append concurrently
	// (uncontested by any revoke).
	for i := 0; i < numRecipients/2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if invites[i] == uuid.Nil {
				return
			}
			err := recipients[i].AcceptInvitation("alice", invites[i], "shared.txt")
			record(err)
		}(i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := alice.RevokeAccess("file1.txt", names[i])
			record(err)
		}(i)
	}
	for i := numRecipients / 2; i < numRecipients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if invites[i] == uuid.Nil {
				return
			}
			err := recipients[i].AcceptInvitation("alice", invites[i], "shared.txt")
			record(err)
			if err != nil {
				return
			}
			_, err = recipients[i].LoadFile("shared.txt")
			record(err)
			err = recipients[i].AppendToFile("shared.txt", []byte("-x"))
			record(err)
			if err == nil {
				atomic.AddInt64(&successfulAppends, 1)
			}
		}(i)
	}
	wg.Wait()

	expectedVersion := uint64(1) + uint64(atomic.LoadInt64(&successfulAppends))
	violation := client.CheckAuthorizationInvariantsForBenchmark(alice, "file1.txt", expectedVersion)

	return authResult{
		Strategy:           strategyName(strategy),
		Attempts:           int(atomic.LoadInt64(&attempts)),
		ExpectedRejects:    int(atomic.LoadInt64(&expectedRejects)),
		UnexpectedErrors:   int(atomic.LoadInt64(&unexpected)),
		InvariantViolation: violation,
	}
}

func strategyName(s client.ConcurrencyStrategy) string {
	for _, sd := range strategies {
		if sd.Value == s {
			return sd.Name
		}
	}
	return "unknown"
}

// ---------------------------------------------------------------------
// Orchestration
// ---------------------------------------------------------------------

const repetitions = 5

func runReps[T any](n int, f func() T) []T {
	out := make([]T, n)
	for i := 0; i < n; i++ {
		out[i] = f()
	}
	return out
}

type environment struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
}

type output struct {
	Environment       environment    `json:"environment"`
	Repetitions       int            `json:"repetitions"`
	WorkloadAppend    []trialSummary `json:"workload_a_append"`
	WorkloadReadHeavy []trialSummary `json:"workload_b_read_heavy"`
	WorkloadIndepFile []trialSummary `json:"workload_c_independent_files"`
	WorkloadReaders   []trialSummary `json:"workload_d_readers"`
	WorkloadAuth      []authResult   `json:"workload_e_authorization"`
	GeneratedAt       string         `json:"generated_at"`
}

func main() {
	outDir := flag.String("out", "benchmarks", "output directory for benchmark-results.json / benchmark-report.md")
	quiet := flag.Bool("quiet", false, "hide per-workload progress and print only the final result")
	color := flag.String("color", "auto", "terminal color: auto, always, or never")
	flag.Parse()

	env := environment{
		OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(),
		NumCPU: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0),
	}
	levels := []int{1, 4, 16, 64}
	const opsPerWorkerA = 10
	const opsPerWorkerB = 20
	const opsPerWorkerC = 10
	const readersD = 64
	const opsPerReaderD = 20
	const filesC = 32
	totalTrials := len(strategies) * (2*len(levels) + 2 + 1 + 1)
	ui, err := newBenchmarkUI(os.Stderr, *quiet, *color, totalTrials)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(2)
	}
	ui.banner(env, repetitions, *outDir)

	out := output{Environment: env, Repetitions: repetitions, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}

	ui.section("A", "Same-file concurrent append", "All workers append to one logical file; detects lost updates.")
	for _, s := range strategies {
		for _, workers := range levels {
			ui.beginTrial(fmt.Sprintf("%-10s · %2d workers", s.Name, workers))
			reps := runReps(repetitions, func() repResult { return workloadAppend(s.Value, workers, opsPerWorkerA) })
			summary := summarize(s.Name, workers, 0, "", reps)
			out.WorkloadAppend = append(out.WorkloadAppend, summary)
			ui.finishTrial(summary)
		}
	}
	ui.endSection()

	ui.section("B", "Same-file read-heavy", "90% reads and 10% appends on one pre-seeded file.")
	for _, s := range strategies {
		for _, workers := range levels {
			ui.beginTrial(fmt.Sprintf("%-10s · %2d workers", s.Name, workers))
			reps := runReps(repetitions, func() repResult { return workloadReadHeavy(s.Value, workers, opsPerWorkerB) })
			summary := summarize(s.Name, workers, 0, "", reps)
			out.WorkloadReadHeavy = append(out.WorkloadReadHeavy, summary)
			ui.finishTrial(summary)
		}
	}
	ui.endSection()

	ui.section("C", "Independent files", "Workers spread across 32 files to measure fine-grained parallelism.")
	for _, s := range strategies {
		for _, workers := range []int{16, 64} {
			ui.beginTrial(fmt.Sprintf("%-10s · %2d workers · %d files", s.Name, workers, filesC))
			reps := runReps(repetitions, func() repResult {
				return workloadIndependentFiles(s.Value, workers, filesC, opsPerWorkerC)
			})
			summary := summarize(s.Name, workers, filesC, "files", reps)
			out.WorkloadIndepFile = append(out.WorkloadIndepFile, summary)
			ui.finishTrial(summary)
		}
	}
	ui.endSection()

	ui.section("D", "Reader concurrency", "64 readers load one shared file without writers.")
	for _, s := range strategies {
		ui.beginTrial(fmt.Sprintf("%-10s · %2d readers", s.Name, readersD))
		reps := runReps(repetitions, func() repResult { return workloadReaders(s.Value, readersD, opsPerReaderD) })
		summary := summarize(s.Name, readersD, 0, "", reps)
		out.WorkloadReaders = append(out.WorkloadReaders, summary)
		ui.finishTrial(summary)
	}
	ui.endSection()

	ui.section("E", "Authorization contention", "Concurrent invitation, acceptance, and revocation invariant checks.")
	for _, s := range strategies {
		ui.beginTrial(fmt.Sprintf("%-10s · single trial", s.Name))
		result := workloadAuthContention(s.Value)
		out.WorkloadAuth = append(out.WorkloadAuth, result)
		ui.finishAuth(result)
	}
	ui.endSection()

	client.SetConcurrencyStrategyForBenchmark(client.StrategySaferCC)

	must(os.MkdirAll(*outDir, 0o755))

	jsonPath := filepath.Join(*outDir, "benchmark-results.json")
	jsonBytes, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(jsonPath, jsonBytes, 0o644))

	mdPath := filepath.Join(*outDir, "benchmark-report.md")
	must(os.WriteFile(mdPath, []byte(renderReport(out)), 0o644))
	ui.complete(out, jsonPath, mdPath)
}
