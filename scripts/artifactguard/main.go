package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 4 {
		fail()
	}
	if err := extract(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fail()
	}
}

func fail() {
	fmt.Fprintln(os.Stderr, "Archive validation or extraction failed")
	os.Exit(1)
}

func extract(archive, destination, inventory string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	seen := make(map[string]bool)
	normalizedSeen := make(map[string]bool)
	var files []string
	var size uint64
	for _, f := range z.File {
		name := f.Name
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.Clean(clean) != clean || strings.HasPrefix(clean, "/") || strings.ContainsAny(clean, "\\\r\n\x00") || strings.Contains(clean, ":") || seen[clean] {
			return errors.New("invalid name")
		}
		seen[clean] = true
		if f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return errors.New("invalid type")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > 256<<20 || size > (512<<20)-f.UncompressedSize64 {
			return errors.New("archive too large")
		}
		size += f.UncompressedSize64
		normalized := clean
		if normalized == "opensearch-plugin.exe" {
			normalized = "opensearch-plugin"
		}
		if normalizedSeen[normalized] {
			return errors.New("duplicate normalized file")
		}
		normalizedSeen[normalized] = true
		if !allowed(normalized) {
			return errors.New("unexpected file")
		}
		files = append(files, normalized)
		target := filepath.Join(destination, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1))
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil || closeOutErr != nil || closeInErr != nil {
			return errors.New("extraction failed")
		}
	}
	sort.Strings(files)
	return os.WriteFile(inventory, []byte(strings.Join(files, "\n")+"\n"), 0600)
}

func allowed(name string) bool {
	switch name {
	case ".tabularium", "README.md", "LICENSE", "NOTICE", "opensearch-plugin", "ppl.bundle.js", "ui/dist/index.js", "ui/dist/raw-source.js", "docs/specification.md", "docs/implementation-plan.md", "docs/compatibility.md", "docs/tickets/001-opensearch-driver-v0.1.0.md", "tests/host/README.md":
		return true
	}
	return strings.HasPrefix(name, "licenses/") || (strings.HasPrefix(name, "locales/") && !strings.Contains(strings.TrimPrefix(name, "locales/"), "/") && strings.HasSuffix(name, ".json"))
}
