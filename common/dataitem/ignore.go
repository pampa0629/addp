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
	if name == "" {
		name = path.Base(candidatePath)
	}
	if name == ".DS_Store" {
		return true, "macos_metadata"
	}
	for _, segment := range strings.Split(candidatePath, "/") {
		if segment == "__MACOSX" {
			return true, "macos_resource_fork"
		}
	}
	return false, ""
}
