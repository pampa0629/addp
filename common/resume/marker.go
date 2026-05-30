package resume

const MarkerVersionV1 = "resume.marker/v1"

// Marker is an opaque, provider-owned checkpoint position.
//
// Orchestrators may persist and pass the marker back to the same provider, but
// must not branch on provider-specific fields inside ReadPosition,
// CommitPosition, Fingerprint, or Payload.
type Marker struct {
	Version        string                 `json:"version"`
	Provider       string                 `json:"provider,omitempty"`
	PositionUnit   string                 `json:"position_unit,omitempty"`
	ReadPosition   map[string]interface{} `json:"read_position,omitempty"`
	CommitPosition map[string]interface{} `json:"commit_position,omitempty"`
	Fingerprint    map[string]interface{} `json:"fingerprint,omitempty"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
}

// Clone returns an independent JSON-like clone suitable for storing markers
// without mutating provider-owned instances.
func (m *Marker) Clone() *Marker {
	if m == nil {
		return nil
	}
	return &Marker{
		Version:        m.Version,
		Provider:       m.Provider,
		PositionUnit:   m.PositionUnit,
		ReadPosition:   cloneMap(m.ReadPosition),
		CommitPosition: cloneMap(m.CommitPosition),
		Fingerprint:    cloneMap(m.Fingerprint),
		Payload:        cloneMap(m.Payload),
	}
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = cloneValue(value)
	}
	return output
}

func cloneValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneMap(typed)
	case []interface{}:
		output := make([]interface{}, len(typed))
		for i, item := range typed {
			output[i] = cloneValue(item)
		}
		return output
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []int64:
		return append([]int64(nil), typed...)
	case []float64:
		return append([]float64(nil), typed...)
	default:
		return value
	}
}
