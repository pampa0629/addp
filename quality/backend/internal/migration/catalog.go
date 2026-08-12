package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
)

var migrationNamePattern = regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.up\.sql$`)

type Catalog struct {
	LatestVersion uint
	Files         []MigrationFile
}

type MigrationFile struct {
	Version  uint
	Name     string
	SHA256   string
	Contents string
}

func ReadCatalog(fsys fs.FS, root string) (Catalog, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return Catalog{}, fmt.Errorf("read quality migration directory %q: %w", root, err)
	}
	files := make([]MigrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return Catalog{}, fmt.Errorf("quality migration directory contains subdirectory %q", entry.Name())
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return Catalog{}, fmt.Errorf("invalid quality migration filename %q", path.Join(root, entry.Name()))
		}
		version, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil {
			return Catalog{}, fmt.Errorf("parse quality migration version from %q: %w", entry.Name(), err)
		}
		contents, err := fs.ReadFile(fsys, path.Join(root, entry.Name()))
		if err != nil {
			return Catalog{}, fmt.Errorf("read quality migration %q: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(contents)
		files = append(files, MigrationFile{
			Version: uint(version), Name: entry.Name(), SHA256: hex.EncodeToString(digest[:]), Contents: string(contents),
		})
	}
	if len(files) == 0 {
		return Catalog{}, fmt.Errorf("quality migration directory %q is empty", root)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	for index, file := range files {
		expected := uint(index + 1)
		if file.Version != expected {
			return Catalog{}, fmt.Errorf("quality migration versions must be contiguous from 000001: expected %06d, found %06d", expected, file.Version)
		}
	}
	return Catalog{LatestVersion: files[len(files)-1].Version, Files: files}, nil
}
