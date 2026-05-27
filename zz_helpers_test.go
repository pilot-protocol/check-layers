// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLayerOrdinal_Branches covers every branch in layerOrdinal.
func TestLayerOrdinal_Branches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		wantN  int
		wantOK bool
	}{
		{"L1", 1, true},
		{"L7", 7, true},
		{"L12", 12, true},
		{"utility", 0, false}, // no L prefix
		{"excluded", 0, false},
		{"", 0, false},
		{"Lbad", 0, false}, // bad sscanf
	}
	for _, tc := range cases {
		n, ok := layerOrdinal(tc.in)
		if n != tc.wantN || ok != tc.wantOK {
			t.Errorf("layerOrdinal(%q) = (%d, %v), want (%d, %v)", tc.in, n, ok, tc.wantN, tc.wantOK)
		}
	}
}

// TestEdgeAllowed_Branches covers the remaining edge-allowance branches
// that the existing simulateCheck-based tests don't reach.
func TestEdgeAllowed_Branches(t *testing.T) {
	t.Parallel()

	t.Run("utility destination is always allowed", func(t *testing.T) {
		ok, _ := edgeAllowed("L7", "utility", nil)
		if !ok {
			t.Error("util dest must be allowed")
		}
	})
	t.Run("L12 may import excluded", func(t *testing.T) {
		ok, _ := edgeAllowed("L12", "excluded", nil)
		if !ok {
			t.Error("L12 → excluded must be allowed")
		}
	})
	t.Run("non-L12 importing excluded is blocked", func(t *testing.T) {
		ok, reason := edgeAllowed("L7", "excluded", nil)
		if ok {
			t.Error("L7 → excluded must be blocked")
		}
		if !strings.Contains(reason, "non-tooling") {
			t.Errorf("reason = %q", reason)
		}
	})
	t.Run("utility source is allowed everywhere", func(t *testing.T) {
		ok, _ := edgeAllowed("utility", "L7", nil)
		if !ok {
			t.Error("utility → L7 must be allowed")
		}
	})
	t.Run("excluded source is allowed", func(t *testing.T) {
		ok, _ := edgeAllowed("excluded", "L7", nil)
		if !ok {
			t.Error("excluded → L7 must be allowed (not validated)")
		}
	})
	t.Run("unknown labels are blocked", func(t *testing.T) {
		ok, reason := edgeAllowed("XYZ", "L7", nil)
		if ok {
			t.Error("unknown source must be blocked")
		}
		if !strings.Contains(reason, "unknown layer") {
			t.Errorf("reason = %q", reason)
		}
	})
	t.Run("same-layer is allowed", func(t *testing.T) {
		ok, _ := edgeAllowed("L7", "L7", nil)
		if !ok {
			t.Error("same-layer must be allowed")
		}
	})
	t.Run("downward consumes constraint", func(t *testing.T) {
		consumesIdx := map[string]map[string]bool{
			"L11": {"L10": true}, // L11 may only import L10
		}
		// L11 → L7 violates the consumes constraint.
		ok, reason := edgeAllowed("L11", "L7", consumesIdx)
		if ok {
			t.Error("layer-skip must be blocked")
		}
		if !strings.Contains(reason, "layer-skip") {
			t.Errorf("reason = %q", reason)
		}
		// L11 → L10 satisfies the constraint.
		ok, _ = edgeAllowed("L11", "L10", consumesIdx)
		if !ok {
			t.Error("L11 → L10 must be allowed under constraint")
		}
	})
}

// TestBuildClassifier_SubpackagesAndDefaults covers the prefix-matching
// branch and the unknown-package fall-through.
func TestBuildClassifier_SubpackagesAndDefaults(t *testing.T) {
	t.Parallel()
	doc := &layersDoc{
		Layers: map[string]layerSpec{
			"L7": {Packages: []string{"example.com/pkg/daemon"}},
		},
		Utilities: layerSpec{Packages: []string{"example.com/internal/util"}},
		Excluded:  layerSpec{Packages: []string{"example.com/legacy"}},
	}
	classify := buildClassifier(doc)
	cases := map[string]string{
		"example.com/pkg/daemon":        "L7",
		"example.com/pkg/daemon/sub":    "L7", // prefix match
		"example.com/internal/util":     "utility",
		"example.com/internal/util/log": "utility", // prefix match
		"example.com/legacy":            "excluded",
		"example.com/unknown":           "",
	}
	for in, want := range cases {
		if got := classify(in); got != want {
			t.Errorf("classify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildConsumesIndex_OnlyLayersWithConstraint covers the two
// branches (empty Consumes is skipped; non-empty entries populate set).
func TestBuildConsumesIndex_OnlyLayersWithConstraint(t *testing.T) {
	t.Parallel()
	doc := &layersDoc{
		Layers: map[string]layerSpec{
			"L11": {Consumes: []string{"L10", "utility"}},
			"L7":  {}, // no Consumes set → must not appear
		},
	}
	idx := buildConsumesIndex(doc)
	if _, ok := idx["L7"]; ok {
		t.Error("L7 with no Consumes must NOT appear in index")
	}
	if !idx["L11"]["L10"] {
		t.Error("L11 consumes set must contain L10")
	}
	if !idx["L11"]["utility"] {
		t.Error("L11 consumes set must contain utility")
	}
}

// TestBuildSideChannelIndex covers the keying-by-Package branch.
func TestBuildSideChannelIndex(t *testing.T) {
	t.Parallel()
	items := map[string]sideChannel{
		"slot-a": {Package: "example.com/pkg/special", Bypasses: []string{"L1"}, Rationale: "x"},
		"slot-b": {Package: "example.com/pkg/another", Bypasses: []string{"L2"}, Rationale: "y"},
	}
	idx := buildSideChannelIndex(items)
	if idx["example.com/pkg/special"].Rationale != "x" {
		t.Error("first entry not re-keyed correctly")
	}
	if idx["example.com/pkg/another"].Rationale != "y" {
		t.Error("second entry not re-keyed correctly")
	}
	if _, ok := idx["slot-a"]; ok {
		t.Error("index keys should be Package paths, not slot names")
	}
}

// TestLoadLayersYAML_Direct exercises the os.ReadFile happy path
// against a temp file.
func TestLoadLayersYAML_Direct(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yamlData := `
layers:
  L1:
    description: foundation
    packages:
      - example.com/proj/pkg/protocol
utilities:
  packages:
    - example.com/proj/internal/util
`
	path := filepath.Join(dir, "layers.yaml")
	if err := os.WriteFile(path, []byte(yamlData), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	doc, err := loadLayersYAML(path)
	if err != nil {
		t.Fatalf("loadLayersYAML: %v", err)
	}
	if doc.Layers["L1"].Packages[0] != "example.com/proj/pkg/protocol" {
		t.Errorf("loaded packages = %v", doc.Layers["L1"].Packages)
	}
	if doc.Utilities.Packages[0] != "example.com/proj/internal/util" {
		t.Errorf("loaded utilities = %v", doc.Utilities.Packages)
	}
}

// TestLoadLayersYAML_NotFoundReturnsError covers the all-paths-failed
// branch when layers.yaml exists nowhere up the tree.
func TestLoadLayersYAML_NotFoundReturnsError(t *testing.T) {
	t.Parallel()
	// /tmp/{random}/no-such-layers.yaml: original path miss + cwd parent
	// walk also misses (because parent dirs of /tmp don't contain layers.yaml
	// unless the repo root does — but t.TempDir is outside the repo).
	dir := t.TempDir()
	bogus := filepath.Join(dir, "no-such-layers.yaml")
	// Change cwd to the temp dir so the cwd-walk fallback also misses.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if _, err := loadLayersYAML(bogus); err == nil {
		t.Error("expected error when layers.yaml is nowhere up the tree")
	}
}

// TestLoadLayersYAML_BadContent surfaces YAML parse errors.
func TestLoadLayersYAML_BadContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "layers.yaml")
	if err := os.WriteFile(path, []byte("this: is: not: valid: yaml: nest"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadLayersYAML(path); err == nil {
		t.Error("expected YAML parse error")
	}
}

// TestBuildTransitionalIndex keys entries by the (from, to) edge.
func TestBuildTransitionalIndex(t *testing.T) {
	t.Parallel()
	items := []transitional{
		{From: "a", To: "b", Owner: "team1"},
		{From: "c", To: "d", Owner: "team2"},
	}
	idx := buildTransitionalIndex(items)
	if idx[edgeKey("a", "b")] != "team1" {
		t.Errorf("owner for a→b = %q", idx[edgeKey("a", "b")])
	}
	if idx[edgeKey("c", "d")] != "team2" {
		t.Errorf("owner for c→d = %q", idx[edgeKey("c", "d")])
	}
}

// TestEdgeKey is trivial but covers the helper.
func TestEdgeKey(t *testing.T) {
	t.Parallel()
	if got := edgeKey("L7", "L1"); got != "L7 → L1" {
		t.Errorf("edgeKey = %q", got)
	}
}
