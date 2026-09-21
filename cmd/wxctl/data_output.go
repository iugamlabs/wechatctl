//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type initDataResult struct {
	Instance string   `json:"instance"`
	DBDir    string   `json:"db_dir"`
	KeysFile string   `json:"keys_file"`
	Keys     int      `json:"keys"`
	Missing  []string `json:"missing"`
}

func writeInitDataText(w io.Writer, r initDataResult) {
	fmt.Fprintf(w, "initialized instance %q\n", r.Instance)
	fmt.Fprintf(w, "  db_dir:     %s\n", r.DBDir)
	fmt.Fprintf(w, "  keys_file:  %s\n", r.KeysFile)
	fmt.Fprintf(w, "  keys:       %d\n", r.Keys)
	fmt.Fprintf(w, "  missing:    %d\n", len(r.Missing))
}
