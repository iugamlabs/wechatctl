package instance

import (
	toml "github.com/pelletier/go-toml/v2"
)

func marshalBundle(bundle ExportBundle) ([]byte, error) {
	return toml.Marshal(bundle)
}

func unmarshalBundle(data []byte, bundle *ExportBundle) error {
	return toml.Unmarshal(data, bundle)
}
