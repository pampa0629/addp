package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FormatPluginManifest struct {
	Descriptor FormatDescriptor `json:"descriptor"`
}

func LoadFormatPluginManifest(path string) (FormatPluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FormatPluginManifest{}, err
	}
	var manifest FormatPluginManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return FormatPluginManifest{}, fmt.Errorf("parse format plugin manifest %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return FormatPluginManifest{}, fmt.Errorf("parse format plugin manifest %s: multiple JSON values", path)
	}
	return manifest, nil
}

func RegisterFormatPluginManifest(path string) (FormatDescriptor, error) {
	manifest, err := LoadFormatPluginManifest(path)
	if err != nil {
		return FormatDescriptor{}, err
	}
	if err := RegisterFormatDescriptor(manifest.Descriptor); err != nil {
		return FormatDescriptor{}, fmt.Errorf("register format plugin manifest %s: %w", path, err)
	}
	descriptor, ok := GetFormatDescriptor(manifest.Descriptor.Format)
	if !ok {
		return FormatDescriptor{}, fmt.Errorf("registered descriptor %s not found", manifest.Descriptor.Format)
	}
	return descriptor, nil
}

func RegisterFormatPluginManifestsFromDir(dir string) ([]FormatDescriptor, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	descriptors := make([]FormatDescriptor, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		descriptor, err := RegisterFormatPluginManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}
