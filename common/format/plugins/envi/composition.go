package envi

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// File is an already observed storage leaf. This package never enumerates or
// opens storage; it cannot turn this fact into an authorization.
type File struct {
	Path string
	Size int64
}

type Pair struct {
	Raster File
	Header File
}

// ResolvePair requires a unique bidirectional pairing in the observed scope.
// It deliberately rejects ambiguity instead of inheriting GDAL search order.
func ResolvePair(headerPath string, header *Header, observed []File) (*Pair, error) {
	if header == nil || header.FileBytes <= 0 || !validPath(headerPath) || !strings.EqualFold(path.Ext(headerPath), ".hdr") {
		return nil, fmt.Errorf("invalid ENVI pair input")
	}
	files := make(map[string]File, len(observed))
	for _, file := range observed {
		if !validPath(file.Path) || file.Size < 0 {
			return nil, fmt.Errorf("invalid ENVI observed file")
		}
		if _, exists := files[file.Path]; exists {
			return nil, fmt.Errorf("duplicate ENVI observed file")
		}
		files[file.Path] = file
	}
	hdr, exists := files[headerPath]
	if !exists || hdr.Size <= 0 || hdr.Size != header.HeaderBytes || hdr.Size > MaxHeaderBytes {
		return nil, fmt.Errorf("ENVI header was not completely observed")
	}
	var candidates []File
	for _, file := range observed {
		if path.Dir(file.Path) == path.Dir(headerPath) && !strings.EqualFold(path.Ext(file.Path), ".hdr") && MatchesHeader(file.Path, headerPath) {
			candidates = append(candidates, file)
		}
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("ENVI header has no unique data file")
	}
	raster := candidates[0]
	if raster.Size != header.FileBytes {
		return nil, fmt.Errorf("ENVI data size differs from the complete declared grid")
	}
	count := 0
	for _, file := range observed {
		if path.Dir(file.Path) == path.Dir(raster.Path) && strings.EqualFold(path.Ext(file.Path), ".hdr") && MatchesHeader(raster.Path, file.Path) {
			count++
		}
		// Native readers can consume these sidecars implicitly. They are not
		// part of the approved pair and must not be silently discarded.
		if path.Dir(file.Path) == path.Dir(raster.Path) {
			base := strings.TrimSuffix(raster.Path, path.Ext(raster.Path))
			for _, sidecar := range []string{raster.Path + ".aux.xml", raster.Path + ".ovr", base + ".sta", base + ".prj", base + ".aux.xml"} {
				if strings.EqualFold(file.Path, sidecar) {
					return nil, fmt.Errorf("unsupported ENVI sidecar composition")
				}
			}
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("ENVI data file has multiple candidate headers")
	}
	return &Pair{Raster: raster, Header: hdr}, nil
}

// MatchesHeader is only a naming observation, never proof of a complete pair.
func MatchesHeader(raster, header string) bool {
	return validPath(raster) && validPath(header) && path.Dir(raster) == path.Dir(header) && !strings.EqualFold(path.Ext(raster), ".hdr") && strings.EqualFold(path.Ext(header), ".hdr") && (strings.EqualFold(header, raster+".hdr") || strings.EqualFold(header, strings.TrimSuffix(raster, path.Ext(raster))+".hdr"))
}

func validPath(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
