package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

type benchmarkUI struct {
	w       io.Writer
	quiet   bool
	color   bool
	current int
	total   int
}

func newBenchmarkUI(w io.Writer, quiet bool, colorMode string, total int) (*benchmarkUI, error) {
	color := false
	switch strings.ToLower(colorMode) {
	case "auto":
		if f, ok := w.(*os.File); ok {
			if info, err := f.Stat(); err == nil {
				color = info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
			}
		}
	case "always":
		color = true
	case "never":
	case "":
		return nil, fmt.Errorf("color mode cannot be empty (use auto, always, or never)")
	default:
		return nil, fmt.Errorf("invalid color mode %q (use auto, always, or never)", colorMode)
	}

	return &benchmarkUI{w: w, quiet: quiet, color: color, total: total}, nil
}

func (ui *benchmarkUI) paint(code, text string) string {
	if !ui.color {
		return text
	}
	return code + text + ansiReset
}

func (ui *benchmarkUI) banner(env environment, reps int, outDir string) {
	if ui.quiet {
		return
	}
	fmt.Fprintln(ui.w, ui.paint(ansiBold+ansiCyan, "SAFER-CC benchmark suite"))
	fmt.Fprintf(ui.w, "%s  %s/%s · %s · %d CPUs · %d repetitions\n",
		ui.paint(ansiDim, "Environment"), env.OS, env.Arch, env.GoVersion, env.NumCPU, reps)
	fmt.Fprintf(ui.w, "%s       %s\n\n", ui.paint(ansiDim, "Output"), outDir)
}

func (ui *benchmarkUI) section(letter, title, detail string) {
	if ui.quiet {
		return
	}
	fmt.Fprintf(ui.w, "%s %s\n", ui.paint(ansiBold+ansiCyan, letter), ui.paint(ansiBold, title))
	fmt.Fprintf(ui.w, "  %s\n", ui.paint(ansiDim, detail))
}

func (ui *benchmarkUI) beginTrial(label string) {
	ui.current++
	if ui.quiet {
		return
	}
	fmt.Fprintf(ui.w, "  %s %-41s ", ui.paint(ansiDim, fmt.Sprintf("[%02d/%02d]", ui.current, ui.total)), label)
}

func (ui *benchmarkUI) finishTrial(result trialSummary) {
	if ui.quiet {
		return
	}
	status := ui.paint(ansiGreen, "PASS")
	if !result.AllRepsVersionOK || result.TotalLostWrites > 0 {
		status = ui.paint(ansiYellow, "WARN")
	}
	fmt.Fprintf(ui.w, "%s  %8s ops/s  p95 %7s  lost %d\n",
		status, formatRate(result.MedianOpsPerSec), formatMicros(result.P95LatencyMicros), result.TotalLostWrites)
}

func (ui *benchmarkUI) finishAuth(result authResult) {
	if ui.quiet {
		return
	}
	status := ui.paint(ansiGreen, "PASS")
	if result.UnexpectedErrors > 0 || result.InvariantViolation != "" {
		status = ui.paint(ansiRed, "FAIL")
	}
	fmt.Fprintf(ui.w, "%s  %3d attempts  %d unexpected errors\n", status, result.Attempts, result.UnexpectedErrors)
}

func (ui *benchmarkUI) endSection() {
	if !ui.quiet {
		fmt.Fprintln(ui.w)
	}
}

func (ui *benchmarkUI) complete(out output, jsonPath, mdPath string) {
	lost := 0
	for _, trials := range [][]trialSummary{
		out.WorkloadAppend,
		out.WorkloadReadHeavy,
		out.WorkloadIndepFile,
		out.WorkloadReaders,
	} {
		for _, trial := range trials {
			if trial.Strategy == "SAFER-CC" {
				lost += trial.TotalLostWrites
			}
		}
	}

	verdict := ui.paint(ansiGreen, "PASS")
	if lost > 0 {
		verdict = ui.paint(ansiRed, "FAIL")
	}
	fmt.Fprintf(ui.w, "%s %s · SAFER-CC lost writes: %d\n", ui.paint(ansiBold, "Complete"), verdict, lost)
	fmt.Fprintf(ui.w, "  JSON    %s\n", jsonPath)
	fmt.Fprintf(ui.w, "  Report  %s\n", mdPath)
}

func formatRate(rate float64) string {
	switch {
	case rate >= 1_000_000:
		return fmt.Sprintf("%.1fM", rate/1_000_000)
	case rate >= 1_000:
		return fmt.Sprintf("%.1fk", rate/1_000)
	default:
		return fmt.Sprintf("%.0f", rate)
	}
}

func formatMicros(micros float64) string {
	if micros >= 1_000 {
		return fmt.Sprintf("%.1fms", micros/1_000)
	}
	return fmt.Sprintf("%.0fµs", micros)
}
