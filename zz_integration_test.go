// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes data to dir/relpath, creating parent dirs as needed.
func writeFile(t *testing.T, dir, relpath, data string) {
	t.Helper()
	full := filepath.Join(dir, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// makeTinyModule creates a self-contained Go module at dir with two
// packages:
//
//	example.com/tinymod/pkg/a (L1)
//	example.com/tinymod/pkg/b (L7) — imports pkg/a (downward, allowed)
//
// And writes a matching layers.yaml at the module root. It is the
// minimal happy-path setup for exercising main() end-to-end without
// triggering os.Exit.
func makeTinyModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "go.mod", "module example.com/tinymod\n\ngo 1.25\n")

	writeFile(t, dir, "pkg/a/a.go", `package a

// Constant pulled by b to create an inter-package edge.
const Name = "a"
`)
	writeFile(t, dir, "pkg/b/b.go", `package b

import "example.com/tinymod/pkg/a"

var _ = a.Name
`)
	writeFile(t, dir, "pkg/b/b_test.go", `package b

import "testing"

func TestSmoke(t *testing.T) {}
`)

	writeFile(t, dir, "layers.yaml", `layers:
  L1:
    description: foundation
    packages:
      - example.com/tinymod/pkg/a
  L7:
    description: daemon-ish
    packages:
      - example.com/tinymod/pkg/b
`)

	return dir
}

// runMainIn changes cwd to dir, resets flag state, sets os.Args, and
// runs main(). Caller is responsible for not invoking the function in
// configurations that would trip os.Exit (i.e. only happy paths).
//
// Returns nothing because main() writes to stderr; the test asserts on
// "did we crash" semantics. If main calls os.Exit, the test process
// terminates and the surrounding `go test` will mark the run as failed
// — which is exactly what we want to surface.
func runMainIn(t *testing.T, dir string, args []string) {
	t.Helper()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}

	prevArgs := os.Args
	prevFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = prevArgs
		flag.CommandLine = prevFlags
	})
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ExitOnError)
	os.Args = args

	main()
}

// TestMain_HappyPath_Imports drives main() end-to-end against a
// minimal in-tmp module with a single allowed downward edge. This
// covers all of main()'s non-error branches plus goList without
// triggering os.Exit.
//
// NOT parallel — mutates cwd, os.Args, flag.CommandLine.
func TestMain_HappyPath_Imports(t *testing.T) {
	dir := makeTinyModule(t)
	runMainIn(t, dir, []string{"check-layers", "./..."})
}

// TestMain_HappyPath_WithTests exercises the --tests branch of main()
// so test-file imports also flow through checkEdge. The tiny module's
// b_test.go imports only stdlib + testing, both of which classify as
// external/stdlib and are dropped silently — clean run.
func TestMain_HappyPath_WithTests(t *testing.T) {
	dir := makeTinyModule(t)
	runMainIn(t, dir, []string{"check-layers", "--tests", "./..."})
}

// TestMain_HappyPath_DefaultArgs covers the `len(args)==0 → ./...`
// fallback branch by passing no positional args.
func TestMain_HappyPath_DefaultArgs(t *testing.T) {
	dir := makeTinyModule(t)
	runMainIn(t, dir, []string{"check-layers"})
}

// TestMain_HappyPath_WithTransitionalWarning seeds layers.yaml with a
// known_transitional entry matching a sibling test-only upward edge,
// so the warnings block executes (and the violations block does not).
//
// Edge configuration:
//
//	pkg/c (L1) test-imports pkg/d (L7) — upward, but flagged transitional
func TestMain_HappyPath_WithTransitionalWarning(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tinymod\n\ngo 1.25\n")
	writeFile(t, dir, "pkg/c/c.go", `package c

const Name = "c"
`)
	writeFile(t, dir, "pkg/c/c_test.go", `package c

import (
	"testing"

	"example.com/tinymod/pkg/d"
)

func TestSmoke(t *testing.T) { _ = d.Name }
`)
	writeFile(t, dir, "pkg/d/d.go", `package d

const Name = "d"
`)
	writeFile(t, dir, "layers.yaml", `layers:
  L1:
    description: foundation
    packages:
      - example.com/tinymod/pkg/c
  L7:
    description: upper
    packages:
      - example.com/tinymod/pkg/d
known_transitional:
  - from: example.com/tinymod/pkg/c
    to: example.com/tinymod/pkg/d
    owner: T-test
`)

	runMainIn(t, dir, []string{"check-layers", "--tests", "./..."})
}

// TestMain_HappyPath_SideChannelBypass exercises the side-channels
// branch: a layer-internal package is whitelisted as a side channel
// from a non-allowed source layer, so the edge passes through the
// `return` inside the side-channel loop without falling through to
// edgeAllowed.
//
// Edge: pkg/upper (L7) imports pkg/lower/sub (L1, side-channel target).
// Without the side-channel exception this would be a downward edge —
// allowed anyway. The bypass+subpackage match still executes the loop
// body for coverage.
func TestMain_HappyPath_SideChannelBypass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tinymod\n\ngo 1.25\n")
	writeFile(t, dir, "pkg/lower/sub/sub.go", `package sub

const Name = "sub"
`)
	writeFile(t, dir, "pkg/upper/upper.go", `package upper

import "example.com/tinymod/pkg/lower/sub"

var _ = sub.Name
`)
	writeFile(t, dir, "layers.yaml", `layers:
  L1:
    description: foundation
    packages:
      - example.com/tinymod/pkg/lower
  L7:
    description: upper
    packages:
      - example.com/tinymod/pkg/upper
side_channels:
  slot-a:
    package: example.com/tinymod/pkg/lower
    bypasses:
      - L7
    rationale: testing
`)

	runMainIn(t, dir, []string{"check-layers", "./..."})
}

// TestGoList_HappyPath runs goList against a tiny module and asserts
// it returns both packages with imports populated. Covers the
// dec.More loop, the seen-dedup branch (single-shot), and the
// no-test path.
//
// NOT parallel — uses os.Chdir.
func TestGoList_HappyPath(t *testing.T) {
	dir := makeTinyModule(t)

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// goList shells out to `go list` in the current process cwd, so
	// we have to chdir. Restore before returning.
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	pkgs, err := goList([]string{"./..."}, false)
	if err != nil {
		t.Fatalf("goList: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d: %+v", len(pkgs), pkgs)
	}
	byPath := map[string]pkgInfo{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
	}
	b, ok := byPath["example.com/tinymod/pkg/b"]
	if !ok {
		t.Fatal("pkg/b missing from result")
	}
	foundA := false
	for _, imp := range b.Imports {
		if imp == "example.com/tinymod/pkg/a" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("pkg/b should import pkg/a; imports = %v", b.Imports)
	}
}

// TestGoList_WithTests asserts the -test path emits TestImports and
// strips the synthetic *.test package. The dedup branch is hit because
// `go list -test` repeats the underlying package alongside its .test
// pseudo-entry.
//
// NOT parallel — uses os.Chdir.
func TestGoList_WithTests(t *testing.T) {
	dir := makeTinyModule(t)

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	pkgs, err := goList([]string{"./..."}, true)
	if err != nil {
		t.Fatalf("goList (withTests): %v", err)
	}

	for _, p := range pkgs {
		if strings.HasSuffix(p.ImportPath, ".test") {
			t.Errorf("synthetic .test pkg leaked: %s", p.ImportPath)
		}
	}

	var b *pkgInfo
	for i := range pkgs {
		if pkgs[i].ImportPath == "example.com/tinymod/pkg/b" {
			b = &pkgs[i]
		}
	}
	if b == nil {
		t.Fatal("pkg/b missing from result")
	}
	foundTesting := false
	for _, imp := range b.TestImports {
		if imp == "testing" {
			foundTesting = true
		}
	}
	if !foundTesting {
		t.Errorf("pkg/b TestImports should include testing; got %v", b.TestImports)
	}
}

// TestGoList_BadArgs covers the cmd.Output() error branch by passing
// an import path that resolves to nothing.
//
// NOT parallel — uses os.Chdir.
func TestGoList_BadArgs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/empty\n\ngo 1.25\n")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	if _, err := goList([]string{"example.com/no/such/package/anywhere"}, false); err == nil {
		t.Error("expected go list error for nonexistent package")
	}
}

// TestLoadLayersYAML_WalkUpFromSubdirectory covers the cwd-walk
// fallback branch: ReadFile(path) fails, walk-up finds layers.yaml in
// a parent directory.
func TestLoadLayersYAML_WalkUpFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "layers.yaml", `layers:
  L1:
    description: foundation
    packages:
      - example.com/walk/pkg/a
`)
	sub := filepath.Join(dir, "deep", "nested", "child")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	// Pass a path that does NOT exist relative to cwd → triggers the
	// walk-up fallback, which should find dir/layers.yaml.
	doc, err := loadLayersYAML("layers.yaml-not-here")
	if err != nil {
		t.Fatalf("loadLayersYAML walk-up: %v", err)
	}
	if doc.Layers["L1"].Packages[0] != "example.com/walk/pkg/a" {
		t.Errorf("walk-up loaded wrong content: %+v", doc.Layers["L1"])
	}
}
