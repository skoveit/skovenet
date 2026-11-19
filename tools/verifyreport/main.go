package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Exit codes. A nonzero status must never be presented as a verification pass.
const (
	exitClean   = 0 // complete logs with at least one test and no failures
	exitProblem = 1 // malformed input, failures, incomplete streams, or no evidence
	exitUsage   = 2 // bad arguments or unreadable input files
)

const usageText = `verifyreport - summarize go test -json logs into Markdown

Usage:
  verifyreport [flags] <tests.log> [more.log ...]
  verifyreport [flags] -        # read one stream from stdin

Reads one or more "go test -json" event logs and writes a Markdown summary,
grouped by package, to stdout. Log contents are treated as untrusted data and
are never executed.

Exit status:
  0  all packages completed, at least one test ran, and nothing failed
  1  malformed input, a failing or incomplete package/test, or no test evidence
  2  usage error or an input file could not be read

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifyreport", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "Verification test report", "heading for the generated report")
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "verifyreport: no input log files given")
		fs.Usage()
		return exitUsage
	}

	agg := newAggregator()
	for _, p := range paths {
		if err := consumePath(agg, p, stdin); err != nil {
			fmt.Fprintf(stderr, "verifyreport: %v\n", err)
			return exitUsage
		}
	}

	rep := agg.finish()
	fmt.Fprint(stdout, rep.render(*title))

	if rep.clean() {
		return exitClean
	}
	return exitProblem
}

func consumePath(agg *aggregator, path string, stdin io.Reader) error {
	if path == "-" {
		return agg.consume("(stdin)", stdin)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return agg.consume(path, f)
}
