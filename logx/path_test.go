package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWithExistingAncestorRebuildsMissingTail(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "missing.log")
	resolved, err := resolveWithExistingAncestor(target)
	if err != nil {
		t.Fatalf("resolveWithExistingAncestor() error = %v", err)
	}
	if filepath.Clean(resolved) != filepath.Clean(target) {
		t.Fatalf("resolved path = %q, want %q", resolved, target)
	}
}

func TestCanonicalLogPathWithinRejectsOutsideTargetWithoutSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside.log")
	resolved, err := canonicalLogPathWithin(root, outside)
	if err == nil {
		t.Fatalf("canonicalLogPathWithin() = %q, nil; want containment error", resolved)
	}
	if !strings.Contains(err.Error(), "escapes log directory") {
		t.Fatalf("canonicalLogPathWithin() error = %v, want containment error", err)
	}
}

func TestResolveWithExistingAncestorResolvesExistingFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "existing.log")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	resolved, err := resolveWithExistingAncestor(target)
	if err != nil {
		t.Fatalf("resolveWithExistingAncestor() error = %v", err)
	}
	if filepath.Clean(resolved) != filepath.Clean(target) {
		t.Fatalf("resolved path = %q, want %q", resolved, target)
	}
}
