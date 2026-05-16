package registry

import (
	"sort"
	"strings"
)

type CapabilityView struct {
	PluginID       string             `json:"plugin_id"`
	Format         Format             `json:"format"`
	DataType       string             `json:"data_type"`
	Layouts        []string           `json:"layouts,omitempty"`
	Identification Identification     `json:"identification,omitempty"`
	Providers      ProviderDescriptor `json:"providers,omitempty"`
	ContentReaders []string           `json:"content_readers,omitempty"`
	Transfer       TransferDescriptor `json:"transfer,omitempty"`
	Parse          bool               `json:"parse,omitempty"`
	Spatial        bool               `json:"spatial,omitempty"`
	EngineFamilies []string           `json:"engine_families,omitempty"`
}

type TransferDescriptor struct {
	Read  bool `json:"read,omitempty"`
	Write bool `json:"write,omitempty"`
}

func ListCapabilityViews() []CapabilityView {
	descriptors := ListDescriptors()
	views := make([]CapabilityView, 0, len(descriptors))
	for _, descriptor := range descriptors {
		views = append(views, CapabilityViewFromDescriptor(descriptor))
	}
	return views
}

func GetCapabilityView(format Format) (CapabilityView, bool) {
	descriptor, ok := GetDescriptor(format)
	if !ok {
		return CapabilityView{}, false
	}
	return CapabilityViewFromDescriptor(descriptor), true
}

func CapabilityViewFromDescriptor(descriptor Descriptor) CapabilityView {
	return CapabilityView{
		PluginID:       descriptor.ID,
		Format:         descriptor.Format,
		DataType:       descriptor.DataType,
		Layouts:        append([]string(nil), descriptor.Layouts...),
		Identification: descriptor.Identification,
		Providers:      descriptor.Providers,
		ContentReaders: append([]string(nil), descriptor.ContentReaders...),
		Transfer: TransferDescriptor{
			Read:  descriptor.TransferRead,
			Write: descriptor.TransferWrite,
		},
		Parse:          descriptor.Parse,
		Spatial:        descriptor.Spatial,
		EngineFamilies: append([]string(nil), descriptor.EngineFamilies...),
	}
}

func ListTransferFormatsForEngineFamily(engineFamily string) []string {
	engineFamily = strings.ToLower(strings.TrimSpace(engineFamily))
	if engineFamily == "" {
		return nil
	}

	descriptors := ListDescriptors()
	formats := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !descriptor.TransferRead && !descriptor.TransferWrite {
			continue
		}
		if containsString(descriptor.EngineFamilies, engineFamily) {
			formats = append(formats, string(descriptor.Format))
		}
	}
	sort.Strings(formats)
	return formats
}
