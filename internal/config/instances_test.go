package config_test

import (
	"testing"

	"github.com/deali/wxctl/internal/config"
)

func TestValidateName(t *testing.T) {
	ok := []string{"a", "work", "dev_1", "my-wechat", "A1_b-2"}
	for _, n := range ok {
		if err := config.ValidateName(n); err != nil {
			t.Fatalf("%q should be valid: %v", n, err)
		}
	}
	bad := []string{"", "中文", "a b", "a.b", "a/b"}
	for _, n := range bad {
		if err := config.ValidateName(n); err == nil {
			t.Fatalf("%q should be invalid", n)
		}
	}
}
