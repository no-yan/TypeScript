package storeharness

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrivateFixtures compares the private name fixtures (6.5, review
// contracts 1): each directory of testdata/private is a program of its
// files, compared by the driver. Strict: a difference fails, unlike the
// corpus. A fixture about an invalid access must have the Pointer report an
// error, and the others none, so that each takes the path it is for.
func TestPrivateFixtures(t *testing.T) {
	withErrors := map[string]bool{
		"1-two-files":       false, // classes of two files with the same local SymbolId
		"2-nested":          true,  // nested classes: the inner #x shadows the outer's
		"3-same-name":       false, // two classes with #x
		"4-static-instance": true,  // static and instance privates; an instance one through the class
		"5-derived":         false, // a base's #x and the derived class's #x
		"6-invalid-access":  true,  // another class's private
	}
	dirs, err := os.ReadDir(filepath.Join("testdata", "private"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != len(withErrors) {
		t.Fatalf("%d fixtures, want %d", len(dirs), len(withErrors))
	}
	for _, dir := range dirs {
		t.Run(dir.Name(), func(t *testing.T) {
			want, ok := withErrors[dir.Name()]
			if !ok {
				t.Fatalf("no expectation for %s", dir.Name())
			}
			entries, err := os.ReadDir(filepath.Join("testdata", "private", dir.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{}
			for _, e := range entries {
				text, err := os.ReadFile(filepath.Join("testdata", "private", dir.Name(), e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				files["/"+e.Name()] = string(text)
			}
			r := Compare(files, stage1Options())
			if r.Excluded != "" {
				t.Fatalf("excluded: %s (%+v)", r.Excluded, r.Detail)
			}
			if r.Class != ClassEqual {
				t.Errorf("%s: %s\nPointer: %s\nStore:   %s", r.Class, r.Detail.Where, r.Detail.Pointer, r.Detail.Store)
			}
			pointer := newPointerProgram(files, stage1Options())
			roots, _ := Roots(pointer, files)
			p := runPointer(pointer, roots, nil)
			errors := len(p.global)
			for _, ds := range p.diags {
				errors += len(ds)
			}
			if (errors > 0) != want {
				t.Errorf("the Pointer reports %d errors, want errors: %v", errors, want)
			}
		})
	}
}
