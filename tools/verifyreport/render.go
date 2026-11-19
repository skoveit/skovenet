package main

import (
	"fmt"
	"sort"
	"strings"
)

// render produces the full Markdown report. Output is deterministic for a given
// set of inputs: packages are sorted by name and all derived lists are ordered.
func (r *report) render(title string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", inlineText(title))
	r.renderSummary(&b)
	r.renderProblems(&b)
	r.renderPackages(&b)
	r.renderSkipped(&b)
	r.renderFailures(&b)
	r.renderNotes(&b)

	return b.String()
}

func (r *report) renderSummary(b *strings.Builder) {
	status := "PASS ✅"
	if !r.clean() {
		status = "FAIL ❌"
	}

	var pass, passNoTest, noFiles, incomplete, failed int
	for _, p := range r.packages {
		switch p.result() {
		case resPass:
			pass++
		case resPassNoTest:
			passNoTest++
		case resNoTestFile:
			noFiles++
		case resIncomplete:
			incomplete++
		case resFail:
			failed++
		}
	}

	files := make([]string, 0, len(r.files))
	for _, f := range r.files {
		files = append(files, inlineCode(f))
	}

	fmt.Fprintf(b, "- **Status:** %s\n", status)
	fmt.Fprintf(b, "- **Log files:** %s\n", strings.Join(files, ", "))
	fmt.Fprintf(b, "- **Test events parsed:** %d\n", r.totalEvents)
	fmt.Fprintf(b, "- **Packages:** %d total — %d passed, %d failed, %d incomplete, %d no test files, %d passed with 0 tests\n",
		len(r.packages), pass, failed, incomplete, noFiles, passNoTest)
	fmt.Fprintf(b, "- **Tests:** %d executed — %d passed, %d failed, %d skipped\n\n",
		r.totalTestPass+r.totalTestFail+r.totalTestSkip, r.totalTestPass, r.totalTestFail, r.totalTestSkip)
}

func (r *report) renderProblems(b *strings.Builder) {
	if r.clean() {
		return
	}
	b.WriteString("## Problems\n\n")
	b.WriteString("> This report is **not** a clean verification pass. Do not treat it as one.\n\n")

	for _, m := range r.malformed {
		fmt.Fprintf(b, "- ❌ Malformed log line %s:%d — %s: %s\n",
			inlineCode(m.file), m.lineNo, inlineText(m.reason), inlineCode(truncate(m.snippet, maxMalformedSnippet)))
	}
	for _, name := range r.failedPkgs {
		fmt.Fprintf(b, "- ❌ Package %s failed.\n", inlineCode(name))
	}
	for _, name := range r.incompletePkgs {
		fmt.Fprintf(b, "- ❌ Package %s started but produced no final result — the log may be truncated.\n", inlineCode(name))
	}
	for _, it := range r.incompleteTests {
		fmt.Fprintf(b, "- ❌ Test %s · %s started but never completed — the log may be truncated.\n",
			inlineCode(it.pkg), inlineCode(it.test))
	}
	if r.noEvidence {
		b.WriteString("- ❌ No executed tests found in any log — there is no test evidence to verify.\n")
	}
	b.WriteString("\n")
}

func (r *report) renderPackages(b *strings.Builder) {
	b.WriteString("## Packages\n\n")
	if len(r.packages) == 0 {
		b.WriteString("_No packages found in the supplied logs._\n\n")
		return
	}
	b.WriteString("| Package | Result | Passed | Failed | Skipped | Elapsed (s) |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: |\n")
	for _, p := range r.packages {
		fmt.Fprintf(b, "| %s | %s | %d | %d | %d | %.2f |\n",
			cellText(p.name), cellText(p.result().label()),
			p.testPass, p.testFail, p.testSkip, p.elapsed)
	}
	b.WriteString("\n")
}

func (r *report) renderSkipped(b *strings.Builder) {
	type sk struct{ pkg, test string }
	var all []sk
	for _, p := range r.packages {
		tests := append([]string(nil), p.skippedTests...)
		sort.Strings(tests)
		for _, t := range tests {
			all = append(all, sk{p.name, t})
		}
	}
	if len(all) == 0 {
		return
	}
	b.WriteString("## Skipped tests\n\n")
	b.WriteString("These tests were explicitly skipped (for example timeout suites under `-short`). ")
	b.WriteString("They are reported, not treated as failures.\n\n")
	for _, s := range all {
		fmt.Fprintf(b, "- %s · %s\n", inlineCode(s.pkg), inlineCode(s.test))
	}
	b.WriteString("\n")
}

func (r *report) renderFailures(b *strings.Builder) {
	type fl struct{ pkg, test string }
	var all []fl
	for _, p := range r.packages {
		tests := append([]string(nil), p.failedTests...)
		sort.Strings(tests)
		for _, t := range tests {
			all = append(all, fl{p.name, t})
		}
	}
	// Package-level failures without an attributable failing test (e.g. build
	// failures) still deserve their captured output.
	var buildFails []*pkgState
	for _, p := range r.packages {
		if p.pkgFail > 0 && len(p.failedTests) == 0 {
			buildFails = append(buildFails, p)
		}
	}
	if len(all) == 0 && len(buildFails) == 0 {
		return
	}
	b.WriteString("## Failures\n\n")
	for _, f := range all {
		fmt.Fprintf(b, "### %s · %s\n\n", inlineCode(f.pkg), inlineCode(f.test))
		p := r.packageByName(f.pkg)
		writeOutputBlock(b, p.testOutput[f.test])
	}
	for _, p := range buildFails {
		fmt.Fprintf(b, "### %s (package-level failure)\n\n", inlineCode(p.name))
		writeOutputBlock(b, p.pkgOutput)
	}
}

func (r *report) packageByName(name string) *pkgState {
	for _, p := range r.packages {
		if p.name == name {
			return p
		}
	}
	return newPkgState(name)
}

func (r *report) renderNotes(b *strings.Builder) {
	b.WriteString("## Notes\n\n")
	b.WriteString("- Counts are taken from `go test -json` terminal events and include subtests.\n")
	b.WriteString("- A passing count means a test executed without failing; it is **not** evidence of behavioral coverage.\n")
	b.WriteString("- Packages shown as `no test files` or `pass (0 tests)` contribute no behavioral evidence.\n")
	b.WriteString("- Log contents are treated as untrusted text: nothing in them is executed and all values are escaped.\n")
}

// writeOutputBlock renders captured output as a fenced code block whose fence is
// long enough that no line of untrusted content can close it early.
func writeOutputBlock(b *strings.Builder, lines []string) {
	if len(lines) == 0 {
		b.WriteString("_No output was captured for this failure._\n\n")
		return
	}
	truncated := false
	if len(lines) > maxRenderedFailLines {
		lines = lines[len(lines)-maxRenderedFailLines:]
		truncated = true
	}
	safe := make([]string, 0, len(lines))
	for _, l := range lines {
		safe = append(safe, sanitize(l))
	}
	fence := fenceFor(safe)
	if truncated {
		b.WriteString("_Showing the last ")
		fmt.Fprintf(b, "%d output lines._\n\n", maxRenderedFailLines)
	}
	b.WriteString(fence)
	b.WriteString("text\n")
	for _, l := range safe {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString(fence)
	b.WriteString("\n\n")
}

// fenceFor returns a run of backticks longer than any backtick run present in
// lines (at least three), so the fence cannot be terminated by the content.
func fenceFor(lines []string) string {
	max := 0
	for _, l := range lines {
		run := 0
		for _, r := range l {
			if r == '`' {
				run++
				if run > max {
					max = run
				}
			} else {
				run = 0
			}
		}
	}
	n := max + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// cellText escapes a string for use inside a Markdown table cell. It neutralises
// the characters that can break a GFM table or trigger inline rendering, while
// leaving common path characters (., /, -) readable.
func cellText(s string) string {
	s = collapseWhitespace(s)
	repl := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"|", "\\|",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "&lt;",
		">", "&gt;",
	)
	return repl.Replace(s)
}

// inlineText escapes a string for use in running Markdown prose.
func inlineText(s string) string {
	s = collapseWhitespace(s)
	repl := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "&lt;",
		">", "&gt;",
	)
	return repl.Replace(s)
}

// inlineCode renders a string as a Markdown code span, escaping the pipe (needed
// inside table cells) and any backtick. The surrounding backticks stop other
// inline Markdown from being interpreted.
func inlineCode(s string) string {
	s = collapseWhitespace(s)
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "|", "\\|")
	return "`" + s + "`"
}

// collapseWhitespace turns any run of whitespace (including newlines, tabs, and
// control characters) into single spaces and trims the result.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// sanitize strips control characters from a single line of captured output,
// keeping tabs for alignment. Used inside fenced code blocks.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
