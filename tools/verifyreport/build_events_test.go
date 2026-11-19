package main

import (
	"strings"
	"testing"
)

func TestBuildWarningsDoNotFailPassingTests(t *testing.T) {
	const warning = `{"ImportPath":"example/foo.test","Action":"build-output","Output":"ld: warning: fixture diagnostic\n"}` + "\n"
	r := parse(t, "macos.log", warning+validStream)
	if !r.clean() || len(r.packages) != 1 {
		t.Fatalf("valid build diagnostic rejected or counted as a passing test package: malformed=%v packages=%d", r.malformed, len(r.packages))
	}
	if only := parse(t, "warnings-only.log", warning); only.clean() || !only.noEvidence {
		t.Fatal("build warnings alone must not count as test evidence")
	}
}

func TestBuildFailureRejectsOtherwisePassingStream(t *testing.T) {
	const build = `{"ImportPath":"example/broken","Action":"build-output","Output":"fixture compilation failed\n"}
{"ImportPath":"example/broken","Action":"build-fail"}
`
	r := parse(t, "build-failure.log", build+validStream)
	if r.clean() || len(r.failedPkgs) != 1 || r.failedPkgs[0] != "example/broken" {
		t.Fatalf("build failure was not recorded: %v", r.failedPkgs)
	}
	if !strings.Contains(r.render("Report"), "fixture compilation failed") {
		t.Fatal("build failure diagnostic was lost")
	}
}

func TestBuildDiagnosticRequiresImportPath(t *testing.T) {
	r := parse(t, "invalid-build.log", `{"Action":"build-output","Output":"warning"}`+"\n"+validStream)
	if r.clean() || len(r.malformed) != 1 {
		t.Fatal("build diagnostic without an import path was accepted")
	}
}
