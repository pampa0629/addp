package dataitem

import (
	"path"
	"strings"
)

type IgnorePolicy interface {
	Ignore(candidate Candidate) (bool, string)
}

type DefaultIgnorePolicy struct{}

func (DefaultIgnorePolicy) Ignore(candidate Candidate) (bool, string) {
	name := strings.TrimSpace(candidate.Name)
	candidatePath := strings.Trim(candidate.Path, "/")
	if name == "" && candidatePath == "" {
		return true, "empty_name"
	}
	if candidate.IsDirectory {
		return true, "directory"
	}
	if ignore, reason := IgnoreSystemEntry(name, candidatePath); ignore {
		return true, reason
	}
	return false, ""
}

func IgnoreSystemEntry(name, candidatePath string) (bool, string) {
	name = strings.TrimSpace(name)
	candidatePath = strings.Trim(candidatePath, "/")
	if name == "" {
		name = path.Base(candidatePath)
	}
	lowerName := strings.ToLower(name)
	switch lowerName {
	case ".ds_store":
		return true, "macos_metadata"
	case "thumbs.db", "desktop.ini":
		return true, "windows_metadata"
	}
	if strings.HasPrefix(name, "._") {
		return true, "macos_resource_fork"
	}
	for _, segment := range strings.Split(candidatePath, "/") {
		if segment == "__MACOSX" {
			return true, "macos_resource_fork"
		}
	}
	return false, ""
}
