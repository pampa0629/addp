package registry

import "github.com/addp/common/datatype"

type SupportView struct {
	PluginID       string             `json:"plugin_id"`
	Format         Format             `json:"format"`
	DataType       datatype.DataType  `json:"data_type"`
	Layouts        []string           `json:"layouts,omitempty"`
	Identification Identification     `json:"identification,omitempty"`
	Providers      ProviderDescriptor `json:"providers,omitempty"`
	ContentReaders []string           `json:"content_readers,omitempty"`
	Parse          bool               `json:"parse,omitempty"`
	Spatial        bool               `json:"spatial,omitempty"`
}

func ListSupportViews() []SupportView {
	descriptors := ListDescriptors()
	views := make([]SupportView, 0, len(descriptors))
	for _, descriptor := range descriptors {
		views = append(views, SupportViewFromDescriptor(descriptor))
	}
	return views
}

func GetSupportView(format Format) (SupportView, bool) {
	descriptor, ok := GetDescriptor(format)
	if !ok {
		return SupportView{}, false
	}
	return SupportViewFromDescriptor(descriptor), true
}

func SupportViewFromDescriptor(descriptor Descriptor) SupportView {
	return SupportView{
		PluginID:       descriptor.ID,
		Format:         descriptor.Format,
		DataType:       descriptor.DataType,
		Layouts:        append([]string(nil), descriptor.Layouts...),
		Identification: descriptor.Identification,
		Providers:      descriptor.Providers,
		ContentReaders: append([]string(nil), descriptor.ContentReaders...),
		Parse:          descriptor.Parse,
		Spatial:        descriptor.Spatial,
	}
}
