package annotate

import (
	"encoding/json"
	"slices"
)

// JSON returns an indented manifest encoding.
func (m Manifest) JSON() ([]byte, error) {
	slices.SortFunc(m.Files, func(a, b ManifestFile) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		default:
			return 0
		}
	})
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
