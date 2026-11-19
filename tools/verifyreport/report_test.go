package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic streams below match the shape of real Go 1.25.4 `go test -json`
// output (verified against a recorded run). Tests never invoke the application
// or the network; they exercise parsing and rendering only.

const validStream = `{"Action":"start","Package":"example/foo"}
{"Action":"run","Package":"example/foo","Test":"TestA"}
{"Action":"output","Package":"example/foo","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Action":"run","Package":"example/foo","Test":"TestA/sub"}
{"Action":"output","Package":"example/foo","Test":"TestA/sub","Output":"=== RUN   TestA/sub\n"}
{"Action":"pass","Package":"example/foo","Test":"TestA/sub","Elapsed":0}
{"Action":"pass","Package":"example/foo","Test":"TestA","Elapsed":0.01}
{"Action":"run","Package":"example/foo","Test":"TestB"}
{"Action":"output","Package":"example/foo","Test":"TestB","Output":"    foo_test.go:3: skipping\n"}
{"Action":"skip","Package":"example/foo","Test":"TestB","Elapsed":0}
{"Action":"output","Package":"example/foo","Output":"PASS\n"}
{"Action":"pass","Package":"example/foo","Elapsed":0.02}
`

func parse(t *testing.T, name, data string) *report {
	t.Helper()
	a := newAggregator()
	if err := a.consume(name, strings.NewReader(data)); err != nil {
		t.Fatalf("consume(%s): %v", name, err)
	}
	return a.finish()
}

func TestValidStream(t *testing.T) {
	r := parse(t, "valid.log", validStream)
	if !r.clean() {
		t.Fatalf("expected clean report, got problems: malformed=%v failed=%v incompletePkgs=%v incompleteTests=%v noEvidence=%v",
			r.malformed, r.failedPkgs, r.incompletePkgs, r.incompleteTests, r.noEvidence)
	}
	if r.totalTestPass != 2 {
		t.Errorf("totalTestPass = %d, want 2 (TestA + TestA/sub)", r.totalTestPass)
	}
	if r.totalTestSkip != 1 {
		t.Errorf("totalTestSkip = %d, want 1 (TestB)", r.totalTestSkip)
	}
	if r.totalTestFail != 0 {
		t.Errorf("totalTestFail = %d, want 0", r.totalTestFail)
	}
	if len(r.packages) != 1 || r.packages[0].result() != resPass {
		t.Fatalf("expected one passing package, got %#v", r.packages)
	}

	md := r.render("Report")
	if !strings.Contains(md, "**Status:** PASS") {
		t.Errorf("rendered report missing PASS status:\n%s", md)
	}
	// An explicit skip must be reported, not treated as a failure.
	if !strings.Contains(md, "## Skipped tests") || !strings.Contains(md, "TestB") {
		t.Errorf("skip not reported:\n%s", md)
	}
	if strings.Contains(md, "## Failures") {
		t.Errorf("clean report should have no Failures section:\n%s", md)
	}
}

func TestPackageLevelFailureWithPassingTests(t *testing.T) {
	const stream = `{"Action":"start","Package":"example/bar"}
{"Action":"run","Package":"example/bar","Test":"TestOK"}
{"Action":"pass","Package":"example/bar","Test":"TestOK","Elapsed":0}
{"Action":"output","Package":"example/bar","Output":"FAIL\texample/bar\t0.1s\n"}
{"Action":"fail","Package":"example/bar","Elapsed":0.1}
`
	r := parse(t, "pkgfail.log", stream)
	if r.clean() {
		t.Fatal("package-level failure must not be clean even though its test passed")
	}
	if r.totalTestPass != 1 {
		t.Errorf("totalTestPass = %d, want 1", r.totalTestPass)
	}
	if len(r.failedPkgs) != 1 || r.failedPkgs[0] != "example/bar" {
		t.Errorf("failedPkgs = %v, want [example/bar]", r.failedPkgs)
	}
	if r.packages[0].result() != resFail {
		t.Errorf("result = %v, want resFail", r.packages[0].result())
	}
	md := r.render("Report")
	if !strings.Contains(md, "## Problems") || !strings.Contains(md, "Package `example/bar` failed") {
		t.Errorf("problem not surfaced:\n%s", md)
	}
}

func TestFailedTest(t *testing.T) {
	const stream = `{"Action":"start","Package":"example/baz"}
{"Action":"run","Package":"example/baz","Test":"TestBad"}
{"Action":"output","Package":"example/baz","Test":"TestBad","Output":"    baz_test.go:10: boom happened\n"}
{"Action":"fail","Package":"example/baz","Test":"TestBad","Elapsed":0}
{"Action":"fail","Package":"example/baz","Elapsed":0.01}
`
	r := parse(t, "fail.log", stream)
	if r.clean() {
		t.Fatal("failing test must not be clean")
	}
	if r.totalTestFail != 1 {
		t.Errorf("totalTestFail = %d, want 1", r.totalTestFail)
	}
	md := r.render("Report")
	if !strings.Contains(md, "## Failures") {
		t.Fatalf("missing Failures section:\n%s", md)
	}
	if !strings.Contains(md, "TestBad") || !strings.Contains(md, "boom happened") {
		t.Errorf("failure output not rendered:\n%s", md)
	}
}

func TestEmptyInput(t *testing.T) {
	r := parse(t, "empty.log", "")
	if r.clean() {
		t.Fatal("empty log must not be clean")
	}
	if !r.noEvidence {
		t.Errorf("expected noEvidence for empty input")
	}
	md := r.render("Report")
	if !strings.Contains(md, "No executed tests found") {
		t.Errorf("empty input should report no evidence:\n%s", md)
	}
}

func TestWhitespaceOnlyInput(t *testing.T) {
	r := parse(t, "ws.log", "\n\n   \n\t\n")
	if r.clean() {
		t.Fatal("whitespace-only log must not be clean")
	}
	if len(r.malformed) != 0 {
		t.Errorf("blank lines should be tolerated, not malformed: %v", r.malformed)
	}
	if !r.noEvidence {
		t.Errorf("expected noEvidence for whitespace-only input")
	}
}

func TestNonJSONInput(t *testing.T) {
	const stream = "this is not json\n{broken\n42\n\"a string\"\n"
	r := parse(t, "junk.log", stream)
	if r.clean() {
		t.Fatal("non-JSON log must not be clean")
	}
	if len(r.malformed) != 4 {
		t.Errorf("malformed count = %d, want 4 (%v)", len(r.malformed), r.malformed)
	}
	md := r.render("Report")
	if !strings.Contains(md, "Malformed log line") {
		t.Errorf("malformed lines not surfaced:\n%s", md)
	}
}

func TestMissingActionIsMalformed(t *testing.T) {
	r := parse(t, "noaction.log", `{"Package":"example/x"}`+"\n")
	if len(r.malformed) != 1 {
		t.Fatalf("malformed = %v, want one entry", r.malformed)
	}
	if !strings.Contains(r.malformed[0].reason, "Action") {
		t.Errorf("reason = %q, want mention of Action", r.malformed[0].reason)
	}
}

func TestTruncatedStream(t *testing.T) {
	// Package started, a test is mid-run, and the stream ends abruptly with no
	// trailing newline — the typical shape of a truncated log.
	const stream = `{"Action":"start","Package":"example/tr"}
{"Action":"run","Package":"example/tr","Test":"TestMid"}
{"Action":"output","Package":"example/tr","Test":"TestMid","Output":"=== RUN   TestMid\n"}`
	r := parse(t, "trunc.log", stream)
	if r.clean() {
		t.Fatal("truncated stream must not be clean")
	}
	if len(r.incompletePkgs) != 1 {
		t.Errorf("incompletePkgs = %v, want one", r.incompletePkgs)
	}
	if len(r.incompleteTests) != 1 || r.incompleteTests[0].test != "TestMid" {
		t.Errorf("incompleteTests = %v, want [TestMid]", r.incompleteTests)
	}
	md := r.render("Report")
	if !strings.Contains(md, "may be truncated") {
		t.Errorf("truncation not surfaced:\n%s", md)
	}
}

func TestNoTestFilesOnly(t *testing.T) {
	const stream = `{"Action":"start","Package":"example/empty1"}
{"Action":"output","Package":"example/empty1","Output":"?   \texample/empty1\t[no test files]\n"}
{"Action":"skip","Package":"example/empty1","Elapsed":0}
{"Action":"start","Package":"example/empty2"}
{"Action":"output","Package":"example/empty2","Output":"?   \texample/empty2\t[no test files]\n"}
{"Action":"skip","Package":"example/empty2","Elapsed":0}
`
	r := parse(t, "notests.log", stream)
	if r.clean() {
		t.Fatal("logs with only no-test-files packages provide no evidence and must not be clean")
	}
	if !r.noEvidence {
		t.Errorf("expected noEvidence")
	}
	for _, p := range r.packages {
		if p.result() != resNoTestFile {
			t.Errorf("package %s result = %v, want resNoTestFile", p.name, p.result())
		}
	}
	md := r.render("Report")
	if !strings.Contains(md, "no test files") {
		t.Errorf("no-test-files packages not labelled:\n%s", md)
	}
}

func TestMixedPackageCompletion(t *testing.T) {
	const stream = `{"Action":"start","Package":"example/done"}
{"Action":"run","Package":"example/done","Test":"TestOne"}
{"Action":"pass","Package":"example/done","Test":"TestOne","Elapsed":0}
{"Action":"pass","Package":"example/done","Elapsed":0.01}
{"Action":"start","Package":"example/mid"}
{"Action":"run","Package":"example/mid","Test":"TestTwo"}
`
	r := parse(t, "mixed.log", stream)
	if r.clean() {
		t.Fatal("a half-finished run must not be clean even if one package completed")
	}
	if r.totalTestPass != 1 {
		t.Errorf("totalTestPass = %d, want 1 (evidence exists)", r.totalTestPass)
	}
	if len(r.incompletePkgs) != 1 || r.incompletePkgs[0] != "example/mid" {
		t.Errorf("incompletePkgs = %v, want [example/mid]", r.incompletePkgs)
	}
	// The completed package should still be classified correctly.
	done := findPkg(r, "example/done")
	if done == nil || done.result() != resPass {
		t.Errorf("example/done = %v, want resPass", done)
	}
}

func TestPassWithZeroTestsIsNotFatalAmongRealTests(t *testing.T) {
	const stream = `{"Action":"start","Package":"example/compileonly"}
{"Action":"output","Package":"example/compileonly","Output":"coverage: 0.0% of statements\n"}
{"Action":"pass","Package":"example/compileonly","Elapsed":0.1}
{"Action":"start","Package":"example/real"}
{"Action":"run","Package":"example/real","Test":"TestReal"}
{"Action":"pass","Package":"example/real","Test":"TestReal","Elapsed":0}
{"Action":"pass","Package":"example/real","Elapsed":0.1}
`
	r := parse(t, "zero.log", stream)
	if !r.clean() {
		t.Fatalf("a 0-test package alongside real tests should still be clean: %+v", r)
	}
	co := findPkg(r, "example/compileonly")
	if co == nil || co.result() != resPassNoTest {
		t.Errorf("compileonly = %v, want resPassNoTest", co)
	}
}

func TestMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	writeFile(t, a, validStream)
	writeFile(t, b, `{"Action":"start","Package":"example/second"}
{"Action":"run","Package":"example/second","Test":"TestZ"}
{"Action":"pass","Package":"example/second","Test":"TestZ","Elapsed":0}
{"Action":"pass","Package":"example/second","Elapsed":0.01}
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{a, b}, strings.NewReader(""), &stdout, &stderr)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitClean, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "example/foo") || !strings.Contains(out, "example/second") {
		t.Errorf("both packages should appear:\n%s", out)
	}
	// 2 passed (foo) + 1 passed (second) = 3; 1 skipped.
	if !strings.Contains(out, "3 passed, 0 failed, 1 skipped") {
		t.Errorf("aggregated totals wrong:\n%s", out)
	}
}

func TestDeterministicAndSortedOutput(t *testing.T) {
	// Packages arrive out of alphabetical order; output must be sorted and stable.
	const stream = `{"Action":"start","Package":"example/zeta"}
{"Action":"run","Package":"example/zeta","Test":"TestZ"}
{"Action":"pass","Package":"example/zeta","Test":"TestZ","Elapsed":0}
{"Action":"pass","Package":"example/zeta","Elapsed":0.01}
{"Action":"start","Package":"example/alpha"}
{"Action":"run","Package":"example/alpha","Test":"TestA"}
{"Action":"pass","Package":"example/alpha","Test":"TestA","Elapsed":0}
{"Action":"pass","Package":"example/alpha","Elapsed":0.01}
`
	r1 := parse(t, "d.log", stream)
	md1 := r1.render("Report")
	r2 := parse(t, "d.log", stream)
	md2 := r2.render("Report")
	if md1 != md2 {
		t.Fatal("render output is not deterministic for identical input")
	}
	if strings.Index(md1, "example/alpha") > strings.Index(md1, "example/zeta") {
		t.Errorf("packages not sorted alphabetically:\n%s", md1)
	}
}

func TestMarkdownEscaping(t *testing.T) {
	// A hostile package/test name must not break the table or inject Markdown.
	const stream = `{"Action":"start","Package":"evil|pkg<b>"}
{"Action":"run","Package":"evil|pkg<b>","Test":"Test*_|x"}
{"Action":"fail","Package":"evil|pkg<b>","Test":"Test*_|x","Elapsed":0}
{"Action":"fail","Package":"evil|pkg<b>","Elapsed":0.01}
`
	r := parse(t, "evil.log", stream)
	md := r.render("Report")
	// The raw pipe from the package name must be escaped inside the table row,
	// otherwise it would create extra columns.
	if strings.Contains(md, "| evil|pkg") {
		t.Errorf("unescaped pipe leaked into table cell:\n%s", md)
	}
	if !strings.Contains(md, "evil\\|pkg") {
		t.Errorf("expected escaped pipe in output:\n%s", md)
	}
	// In table cells, angle brackets are HTML-entity escaped. In prose they are
	// wrapped in a code span (which renders content literally); neither path may
	// emit a raw, unescaped <b> tag outside of a backtick code span.
	if !strings.Contains(md, "evil\\|pkg&lt;b&gt;") {
		t.Errorf("table cell did not entity-escape angle brackets:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if idx := strings.Index(line, "<b>"); idx >= 0 {
			if strings.Count(line[:idx], "`")%2 == 0 {
				t.Errorf("raw <b> appeared outside a code span: %q", line)
			}
		}
	}
}

func TestFailureOutputCannotEscapeCodeFence(t *testing.T) {
	// Captured output containing a triple backtick must not break out of the
	// fenced code block in the Failures section.
	stream := `{"Action":"start","Package":"example/inj"}
{"Action":"run","Package":"example/inj","Test":"TestInj"}
{"Action":"output","Package":"example/inj","Test":"TestInj","Output":"before\n"}
{"Action":"output","Package":"example/inj","Test":"TestInj","Output":"` + "```" + `\n"}
{"Action":"output","Package":"example/inj","Test":"TestInj","Output":"## injected heading\n"}
{"Action":"fail","Package":"example/inj","Test":"TestInj","Elapsed":0}
{"Action":"fail","Package":"example/inj","Elapsed":0.01}
`
	r := parse(t, "inj.log", stream)
	md := r.render("Report")
	// The fence chosen must be longer than the backtick run in the content.
	if !strings.Contains(md, "````text") {
		t.Errorf("expected a 4-backtick fence to contain 3-backtick content:\n%s", md)
	}
}

func TestFenceFor(t *testing.T) {
	cases := []struct {
		lines []string
		want  string
	}{
		{nil, "```"},
		{[]string{"plain"}, "```"},
		{[]string{"a ``` b"}, "````"},
		{[]string{"````", "``"}, "`````"},
	}
	for _, c := range cases {
		if got := fenceFor(c.lines); got != c.want {
			t.Errorf("fenceFor(%q) = %q, want %q", c.lines, got, c.want)
		}
	}
}

func TestCollapseWhitespaceAndSanitize(t *testing.T) {
	if got := collapseWhitespace("a\tb\n  c\x00d"); got != "a b c d" {
		t.Errorf("collapseWhitespace = %q", got)
	}
	if got := sanitize("ok\x1b[31mred\x00"); got != "ok [31mred " {
		t.Errorf("sanitize = %q", got)
	}
}

// --- run() exit-code behaviour ---

func TestRunNoArgsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "no input log files") {
		t.Errorf("missing usage message: %s", stderr.String())
	}
}

func TestRunMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "nope.log")}, strings.NewReader(""), &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunCleanFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.log")
	writeFile(t, p, validStream)
	var stdout, stderr bytes.Buffer
	if code := run([]string{p}, strings.NewReader(""), &stdout, &stderr); code != exitClean {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitClean, stderr.String())
	}
}

func TestRunProblemFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.log")
	writeFile(t, p, `{"Action":"start","Package":"example/x"}
{"Action":"run","Package":"example/x","Test":"T"}
{"Action":"fail","Package":"example/x","Test":"T","Elapsed":0}
{"Action":"fail","Package":"example/x","Elapsed":0}
`)
	var stdout, stderr bytes.Buffer
	if code := run([]string{p}, strings.NewReader(""), &stdout, &stderr); code != exitProblem {
		t.Fatalf("exit = %d, want %d", code, exitProblem)
	}
}

func TestRunStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-"}, strings.NewReader(validStream), &stdout, &stderr); code != exitClean {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitClean, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(stdin)") {
		t.Errorf("stdin source not labelled:\n%s", stdout.String())
	}
}

// helpers

func findPkg(r *report, name string) *pkgState {
	for _, p := range r.packages {
		if p.name == name {
			return p
		}
	}
	return nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
