package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("broken input") }
func TestSaveAndFailureCleanup(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Ready(); err != nil {
		t.Fatal(err)
	}
	key, size, err := s.Save(strings.NewReader("hello"), 5)
	if err != nil || size != 5 {
		t.Fatalf("save: %d %v", size, err)
	}
	f, err := s.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(data) != "hello" {
		t.Fatal("content mismatch")
	}
	if err = s.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err = s.Remove(key); err != nil {
		t.Fatal("remove must be idempotent")
	}
	if _, _, err = s.Save(strings.NewReader("too large"), 2); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, _, err = s.Save(io.MultiReader(strings.NewReader("partial"), brokenReader{}), 100); err == nil {
		t.Fatal("expected failure")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed uploads leaked files: %v %v", files, err)
	}
	s.Close()
	if _, _, err = s.Save(strings.NewReader("x"), 10); err == nil {
		t.Fatal("closed storage must fail")
	}
}
func TestRootRejectsTraversalAndSymlink(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "media")
	if err := os.WriteFile(filepath.Join(parent, "secret"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Open("../secret"); err == nil {
		t.Fatal("path traversal allowed")
	}
	if err = os.Symlink(filepath.Join(parent, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open("escape"); err == nil {
		t.Fatal("symlink escape allowed")
	}
}
