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
	Root          string
	LatestVersion uint
	Names         []string
	Files         []MigrationFile
}

type MigrationFile struct {
	Version uint
	Name    string
	SHA256  string
}

func ReadCatalog(fsys fs.FS, root string) (Catalog, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return Catalog{}, fmt.Errorf("read migration directory %q: %w", root, err)
	}

	files := make([]MigrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return Catalog{}, fmt.Errorf("migration directory %q contains subdirectory %q", root, entry.Name())
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return Catalog{}, fmt.Errorf("invalid migration filename %q", path.Join(root, entry.Name()))
		}
		version, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil {
			return Catalog{}, fmt.Errorf("parse migration version from %q: %w", entry.Name(), err)
		}
		data, err := fs.ReadFile(fsys, path.Join(root, entry.Name()))
		if err != nil {
			return Catalog{}, fmt.Errorf("read migration %q: %w", path.Join(root, entry.Name()), err)
		}
		digest := sha256.Sum256(data)
		files = append(files, MigrationFile{
			Version: uint(version),
			Name:    entry.Name(),
			SHA256:  hex.EncodeToString(digest[:]),
		})
	}

	if len(files) == 0 {
		return Catalog{}, fmt.Errorf("migration directory %q is empty", root)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	names := make([]string, 0, len(files))
	for index, migrationFile := range files {
		expected := uint(index + 1)
		if migrationFile.Version != expected {
			return Catalog{}, fmt.Errorf("migration versions must be contiguous from 000001: expected %06d, found %06d", expected, migrationFile.Version)
		}
		names = append(names, migrationFile.Name)
	}

	return Catalog{Root: root, LatestVersion: files[len(files)-1].Version, Names: names, Files: files}, nil
}
