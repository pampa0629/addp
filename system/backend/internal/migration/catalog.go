package migration

import (
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
}

func ReadCatalog(fsys fs.FS, root string) (Catalog, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return Catalog{}, fmt.Errorf("read migration directory %q: %w", root, err)
	}

	names := make([]string, 0, len(entries))
	versions := make([]uint, 0, len(entries))
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
		names = append(names, entry.Name())
		versions = append(versions, uint(version))
	}

	if len(names) == 0 {
		return Catalog{}, fmt.Errorf("migration directory %q is empty", root)
	}

	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for index, version := range versions {
		expected := uint(index + 1)
		if version != expected {
			return Catalog{}, fmt.Errorf("migration versions must be contiguous from 000001: expected %06d, found %06d", expected, version)
		}
	}

	return Catalog{Root: root, LatestVersion: versions[len(versions)-1], Names: names}, nil
}
