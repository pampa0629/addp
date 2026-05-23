package datatype

// NativeAllowedKeys declares the source-specific table native keys that may
// pass through to TableInfo.Native.
type NativeAllowedKeys map[string]struct{}

// NewNativeAllowedKeys builds a key set for filtering source native facts.
func NewNativeAllowedKeys(keys ...string) NativeAllowedKeys {
	allowed := make(NativeAllowedKeys, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
	}
	return allowed
}

// FilterTableNative returns a shallow copy of native facts whose keys are in
// allowed. Empty keys, nil values and unknown keys are dropped.
func FilterTableNative(native map[string]interface{}, allowed NativeAllowedKeys) map[string]interface{} {
	if len(native) == 0 || len(allowed) == 0 {
		return nil
	}
	filtered := make(map[string]interface{}, len(native))
	for key, value := range native {
		if key == "" || value == nil {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
