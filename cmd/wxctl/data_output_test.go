//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/star-plan/wechatctl/internal/runtime"
	"github.com/star-plan/wechatctl/internal/wxdata"
)

func TestClassifyRunErrorPrintsOuterWrap(t *testing.T) {
	err := fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force work: %w", wxdata.ErrNoKeys)
	code, printErr := classifyRunError(err)
	if code != 3 || !printErr {
		t.Fatalf("code=%d print=%v", code, printErr)
	}
	outer := err.Error()
	if !strings.Contains(outer, "--force") {
		t.Fatalf("outer err must contain --force: %q", outer)
	}
	if strings.Contains(outer, "wechat exited") {
		t.Fatalf("must not look like ExitError: %q", outer)
	}
	var we *wxdata.Error
	if !errors.As(err, &we) || we.Code != 3 {
		t.Fatal("errors.As should yield Code 3")
	}
	if we.Error() != "keys not found" {
		t.Fatalf("sentinel Error()=%q (printing this would drop --force)", we.Error())
	}
	if !strings.Contains(outer, we.Error()) {
		t.Fatalf("outer should wrap sentinel: outer=%q sentinel=%q", outer, we.Error())
	}
}

func TestClassifyRunErrorExitErrorSilent(t *testing.T) {
	err := &runtime.ExitError{Code: 42}
	code, printErr := classifyRunError(err)
	if code != 42 || printErr {
		t.Fatalf("ExitError should stay silent: code=%d print=%v", code, printErr)
	}
}

func TestWriteJSONIndentAndMissingArray(t *testing.T) {
	var buf bytes.Buffer
	res := initDataResult{
		Instance: "work",
		DBDir:    "/db",
		KeysFile: "/keys",
		Keys:     2,
		Missing:  []string{},
	}
	if err := writeJSON(&buf, res); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("JSON must end with newline")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["missing"].([]any); !ok {
		t.Fatalf("missing should be array, got %T %v", got["missing"], got["missing"])
	}
	if strings.Contains(out, `\u003c`) {
		t.Fatal("HTML escaped")
	}
}

func TestWriteInitDataText(t *testing.T) {
	var buf bytes.Buffer
	writeInitDataText(&buf, initDataResult{
		Instance: "work",
		DBDir:    "/db",
		KeysFile: "/k",
		Keys:     16,
		Missing:  []string{"a", "b"},
	})
	got := buf.String()
	for _, want := range []string{`initialized instance "work"`, "keys:       16", "missing:    2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
