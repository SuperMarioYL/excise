package main

import (
	"strings"
	"testing"
)

// fix-version-constant-stale: `var version` (cmd/excise/main.go:31) drifted
// to "0.5.0" through v0.6.0/v0.7.0. goreleaser injects the tag via ldflags for
// gh releases, but the documented install path (README.md:80
// `go install ...@latest`, README.md:87 `go build`, site.json hero.command)
// does NOT inject it, so `excise --version` printed "0.5.0" on a v0.7.0 binary.
// This test pins the in-source constant to the shipped version so a future
// silent drift fails CI.
func TestVersionConstantMatchesShippedRelease(t *testing.T) {
	want := "0.9.0"
	if version != want {
		t.Errorf("version constant = %q, want %q (bump it when cutting a release; goreleaser injects the tag only for gh releases, not go install/go build)", version, want)
	}
	// It must be a bare semver (no leading v) — cobra renders it as
	// "excise version <version>" and the README/install docs assume this shape.
	if !looksLikeSemver(version) {
		t.Errorf("version %q is not a bare semver (expected e.g. 0.8.0)", version)
	}
}

// looksLikeSemver is a local x.y.z check so this test does not import a
// semver library for one assertion.
func looksLikeSemver(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
