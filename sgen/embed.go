package main

import "embed"

// These archives are created by the Makefile before building sgen.
// toolchain.*: Go compiler for the host platform (from go.dev)
// source.tar.gz:    Agent source code + vendored dependencies

//go:embed assets/toolchain.*
var toolchainFS embed.FS

//go:embed assets/source.tar.gz
var sourceArchive []byte
