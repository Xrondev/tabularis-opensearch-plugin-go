package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveValidation(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		valid   bool
	}{
		{"valid", []string{"README.md", "licenses/dependency/LICENSE"}, true},
		{"parent", []string{"../README.md"}, false},
		{"nested parent", []string{"licenses/../../README.md"}, false},
		{"absolute", []string{"/README.md"}, false},
		{"backslash", []string{"licenses\\secret"}, false},
		{"duplicate", []string{"README.md", "README.md"}, false},
		{"unexpected", []string{"credentials.json"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "test.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			w := zip.NewWriter(f)
			for _, name := range tc.entries {
				entry, err := w.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte("test")); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			err = extract(archive, filepath.Join(dir, "out"), filepath.Join(dir, "inventory"))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
