// Package main implements verifyreport, a small standard-library CLI that
// turns `go test -json` event logs into a concise Markdown summary.
//
// The tool treats log contents as untrusted data: it never executes anything
// found in a log and escapes everything it renders into Markdown. Its purpose
// is to make test evidence auditable and to make MISSING or INCOMPLETE evidence
// obvious, so a caller cannot present a truncated or empty log as a clean pass.
package main

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// event mirrors the subset of fields emitted by `go test -json`
// (cmd/internal/test2json TestEvent) that this tool needs. Unknown fields are
// ignored by encoding/json.
type event struct {
	Action     string  `json:"Action"`
	Package    string  `json:"Package"`
	Test       string  `json:"Test"`
	Elapsed    float64 `json:"Elapsed"`
	Output     string  `json:"Output"`
	ImportPath string  `json:"ImportPath"`
}

// Limits that bound memory and keep output readable when logs are large or
// hostile. They are intentionally generous for real failures but finite.
const (
	maxStoredOutputLines = 60  // per test / per package captured output lines
	maxRenderedFailLines = 40  // output lines shown per failed test
	maxMalformedSnippet  = 160 // characters shown for a malformed line
)

// malformed records a log line that could not be parsed as a test event.
type malformed struct {
	file    string
	lineNo  int
	snippet string
	reason  string
}

// pkgState accumulates everything observed for a single package name. The same
// package may legitimately appear across multiple input files (for example a
// fast and a full run); counts are aggregated and completeness is tracked by
// balancing start/terminal and run/terminal events.
type pkgState struct {
	name string

	started    int // package-level "start" events
	terminated int // package-level pass+fail+skip events

	pkgPass int
	pkgFail int
	pkgSkip int // package-level skip == "[no test files]"

	elapsed float64

	testPass int
	testFail int
	testSkip int

	running map[string]int // test name -> net (run minus terminal) count

	failedTests  []string
	skippedTests []string

	testOutput map[string][]string // captured output lines per test (bounded)
	pkgOutput  []string            // package-level output lines (bounded)
}

func newPkgState(name string) *pkgState {
	return &pkgState{
		name:       name,
		running:    map[string]int{},
		testOutput: map[string][]string{},
	}
}

// aggregator consumes one or more log streams and builds the per-package state.
type aggregator struct {
	files       []string
	totalEvents int
	malformed   []malformed

	order       []string // package names in first-seen order
	packages    map[string]*pkgState
	buildOutput map[string][]string
}

func newAggregator() *aggregator {
	return &aggregator{packages: map[string]*pkgState{}, buildOutput: map[string][]string{}}
}

func (a *aggregator) pkg(name string) *pkgState {
	p, ok := a.packages[name]
	if !ok {
		p = newPkgState(name)
		a.packages[name] = p
		a.order = append(a.order, name)
	}
	return p
}

// consume reads a single stream line by line. It uses a bufio.Reader (not a
// Scanner) so that arbitrarily long output lines and a final line without a
// trailing newline are handled correctly — the latter is how a truncated stream
// typically appears.
func (a *aggregator) consume(name string, r io.Reader) error {
	a.files = append(a.files, name)
	br := bufio.NewReader(r)
	lineNo := 0
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			lineNo++
			a.processLine(name, lineNo, strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (a *aggregator) processLine(file string, lineNo int, s string) {
	if strings.TrimSpace(s) == "" {
		return // tolerate blank lines; go test -json does not emit them
	}
	var e event
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		a.malformed = append(a.malformed, malformed{file, lineNo, s, err.Error()})
		return
	}
	if e.Action == "" {
		a.malformed = append(a.malformed, malformed{file, lineNo, s, "missing Action field"})
		return
	}
	// Go emits build diagnostics separately from test2json events. They use
	// ImportPath instead of Package and may be harmless linker warnings.
	if e.Action == "build-output" || e.Action == "build-fail" {
		if e.ImportPath == "" {
			a.malformed = append(a.malformed, malformed{file, lineNo, s, "missing ImportPath field"})
			return
		}
		a.totalEvents++
		if e.Action == "build-output" {
			a.buildOutput[e.ImportPath] = appendBounded(a.buildOutput[e.ImportPath], e.Output)
		} else {
			p := a.pkg(e.ImportPath)
			p.pkgFail++
			p.pkgOutput = append(p.pkgOutput, a.buildOutput[e.ImportPath]...)
		}
		return
	}
	if e.Package == "" {
		a.malformed = append(a.malformed, malformed{file, lineNo, s, "missing Package field"})
		return
	}
	a.totalEvents++
	a.apply(e)
}

func (a *aggregator) apply(e event) {
	p := a.pkg(e.Package)
	if e.Test == "" {
		a.applyPackage(p, e)
		return
	}
	a.applyTest(p, e)
}

func (a *aggregator) applyPackage(p *pkgState, e event) {
	switch e.Action {
	case "start":
		p.started++
	case "pass":
		p.terminated++
		p.pkgPass++
		p.elapsed += e.Elapsed
	case "fail":
		p.terminated++
		p.pkgFail++
		p.elapsed += e.Elapsed
	case "skip":
		p.terminated++
		p.pkgSkip++
		p.elapsed += e.Elapsed
	case "output":
		p.pkgOutput = appendBounded(p.pkgOutput, e.Output)
	}
}

func (a *aggregator) applyTest(p *pkgState, e event) {
	switch e.Action {
	case "run":
		p.running[e.Test]++
	case "pass":
		p.testPass++
		decRunning(p.running, e.Test)
	case "fail":
		p.testFail++
		decRunning(p.running, e.Test)
		p.failedTests = append(p.failedTests, e.Test)
	case "skip":
		p.testSkip++
		decRunning(p.running, e.Test)
		p.skippedTests = append(p.skippedTests, e.Test)
	case "output":
		p.testOutput[e.Test] = appendBounded(p.testOutput[e.Test], e.Output)
	}
}

func decRunning(m map[string]int, test string) {
	if m[test] > 0 {
		m[test]--
		if m[test] == 0 {
			delete(m, test)
		}
	}
}

// appendBounded keeps at most maxStoredOutputLines lines, dropping the oldest
// so that the most recent output (where failures usually surface) is retained.
func appendBounded(lines []string, out string) []string {
	out = strings.TrimRight(out, "\n")
	lines = append(lines, out)
	if len(lines) > maxStoredOutputLines {
		lines = lines[len(lines)-maxStoredOutputLines:]
	}
	return lines
}

// pkgResult is the derived classification of a single package.
type pkgResult int

const (
	resPass       pkgResult = iota // tests ran and all passed
	resPassNoTest                  // package passed but ran zero tests
	resNoTestFile                  // package reported "[no test files]"
	resIncomplete                  // started without a terminal (or test left running)
	resFail                        // package or a test failed
)

func (r pkgResult) label() string {
	switch r {
	case resPass:
		return "pass"
	case resPassNoTest:
		return "pass (0 tests)"
	case resNoTestFile:
		return "no test files"
	case resIncomplete:
		return "INCOMPLETE"
	case resFail:
		return "FAIL"
	}
	return "unknown"
}

func (p *pkgState) incompleteTests() []string {
	if len(p.running) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.running))
	for t := range p.running {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (p *pkgState) tests() int { return p.testPass + p.testFail + p.testSkip }

func (p *pkgState) result() pkgResult {
	switch {
	case p.pkgFail > 0 || p.testFail > 0:
		return resFail
	case p.started > p.terminated || len(p.running) > 0:
		return resIncomplete
	case p.pkgSkip > 0 && p.tests() == 0 && p.pkgPass == 0:
		return resNoTestFile
	case p.pkgPass > 0 && p.tests() == 0:
		return resPassNoTest
	default:
		return resPass
	}
}

// report is the finished, immutable view used for rendering.
type report struct {
	files       []string
	totalEvents int
	malformed   []malformed
	packages    []*pkgState // sorted by name

	totalTestPass int
	totalTestFail int
	totalTestSkip int

	// derived problem signals
	incompletePkgs  []string
	failedPkgs      []string
	incompleteTests []incompleteTest
	noEvidence      bool
}

type incompleteTest struct {
	pkg  string
	test string
}

// finish computes the report in a deterministic order.
func (a *aggregator) finish() *report {
	names := append([]string(nil), a.order...)
	sort.Strings(names)

	r := &report{
		files:       a.files,
		totalEvents: a.totalEvents,
		malformed:   a.malformed,
	}
	for _, name := range names {
		p := a.packages[name]
		r.packages = append(r.packages, p)
		r.totalTestPass += p.testPass
		r.totalTestFail += p.testFail
		r.totalTestSkip += p.testSkip

		switch p.result() {
		case resFail:
			r.failedPkgs = append(r.failedPkgs, name)
		case resIncomplete:
			r.incompletePkgs = append(r.incompletePkgs, name)
		}
		for _, t := range p.incompleteTests() {
			r.incompleteTests = append(r.incompleteTests, incompleteTest{pkg: name, test: t})
		}
	}

	r.noEvidence = (r.totalTestPass + r.totalTestFail + r.totalTestSkip) == 0
	return r
}

// clean reports whether the logs represent a trustworthy, complete pass.
func (r *report) clean() bool {
	return len(r.malformed) == 0 &&
		len(r.failedPkgs) == 0 &&
		len(r.incompletePkgs) == 0 &&
		len(r.incompleteTests) == 0 &&
		r.totalTestFail == 0 &&
		!r.noEvidence
}
