package models

type CapabilitiesView struct {
	Summary  []CapabilityViewBadge   `json:"summary"`
	Sections []CapabilityViewSection `json:"sections"`
	JSONView []CapabilityJSONNode    `json:"json_view"`
}

type CapabilityViewBadge struct {
	ID       string `json:"id"`
	LabelKey string `json:"label_key"`
	Value    string `json:"value,omitempty"`
	ValueKey string `json:"value_key,omitempty"`
	Status   string `json:"status,omitempty"`
}

type CapabilityViewSection struct {
	ID             string               `json:"id"`
	TitleKey       string               `json:"title_key"`
	DescriptionKey string               `json:"description_key,omitempty"`
	Status         string               `json:"status,omitempty"`
	Path           []CapabilityPathNode `json:"path,omitempty"`
	Items          []CapabilityViewItem `json:"items,omitempty"`
}

type CapabilityPathNode struct {
	ID       string              `json:"id"`
	LabelKey string              `json:"label_key,omitempty"`
	Value    string              `json:"value,omitempty"`
	Tags     []CapabilityViewTag `json:"tags,omitempty"`
}

type CapabilityViewItem struct {
	ID             string              `json:"id"`
	LabelKey       string              `json:"label_key"`
	DescriptionKey string              `json:"description_key,omitempty"`
	ReasonKey      string              `json:"reason_key,omitempty"`
	Value          string              `json:"value,omitempty"`
	ValueKey       string              `json:"value_key,omitempty"`
	Status         string              `json:"status,omitempty"`
	Tags           []CapabilityViewTag `json:"tags,omitempty"`
}

type CapabilityViewTag struct {
	ID       string `json:"id"`
	LabelKey string `json:"label_key,omitempty"`
	Value    string `json:"value,omitempty"`
	Type     string `json:"type,omitempty"`
}

type CapabilityJSONNode struct {
	Key      string               `json:"key"`
	Value    string               `json:"value,omitempty"`
	Children []CapabilityJSONNode `json:"children,omitempty"`
}
