package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBenchmarkUIProgressAndResult(t *testing.T) {
	var out bytes.Buffer
	ui, err := newBenchmarkUI(&out, false, "never", 2)
	if err != nil {
		t.Fatal(err)
	}

	ui.banner(environment{OS: "test-os", Arch: "test-arch", GoVersion: "go-test", NumCPU: 4}, 5, "results")
	ui.section("A", "Append", "detail")
	ui.beginTrial("SAFER-CC · 4 workers")
	ui.finishTrial(trialSummary{MedianOpsPerSec: 12_345, P95LatencyMicros: 1_250, AllRepsVersionOK: true})
	ui.beginTrial("NoCC · 4 workers")
	ui.finishTrial(trialSummary{MedianOpsPerSec: 999, P95LatencyMicros: 25, TotalLostWrites: 2})

	got := out.String()
	for _, want := range []string{
		"SAFER-CC benchmark suite",
		"test-os/test-arch",
		"[01/02]",
		"12.3k ops/s",
		"1.2ms",
		"PASS",
		"[02/02]",
		"WARN",
		"lost 2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestBenchmarkUIQuietMode(t *testing.T) {
	var out bytes.Buffer
	ui, err := newBenchmarkUI(&out, true, "never", 1)
	if err != nil {
		t.Fatal(err)
	}

	ui.banner(environment{}, 1, "results")
	ui.section("A", "Append", "detail")
	ui.beginTrial("SAFER-CC")
	ui.finishTrial(trialSummary{})
	if out.Len() != 0 {
		t.Fatalf("quiet progress wrote %q", out.String())
	}

	ui.complete(output{}, "results/results.json", "results/report.md")
	if !strings.Contains(out.String(), "results/report.md") {
		t.Fatalf("quiet mode should still print final paths: %q", out.String())
	}
}

func TestBenchmarkUIRejectsInvalidColorMode(t *testing.T) {
	if _, err := newBenchmarkUI(&bytes.Buffer{}, false, "sometimes", 1); err == nil {
		t.Fatal("expected invalid color mode error")
	}
}

func TestFormatMetrics(t *testing.T) {
	cases := map[string]string{
		formatRate(999):       "999",
		formatRate(12_345):    "12.3k",
		formatRate(2_500_000): "2.5M",
		formatMicros(42):      "42µs",
		formatMicros(1_250):   "1.2ms",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
