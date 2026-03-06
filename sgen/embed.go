package main

import _ "embed"

// These archives are created by the Makefile before building sgen.
// toolchain.tar.gz: Go compiler for the host platform (from go.dev)
// source.tar.gz:    Agent source code + vendored dependencies

//go:embed assets/toolchain.tar.gz
var toolchainArchive []byte

//go:embed assets/source.tar.gz
var sourceArchive []byte
