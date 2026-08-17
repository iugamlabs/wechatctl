package config_test

import (
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/star-plan/wechatctl/internal/config"
)

func TestInstanceWindowsFieldsRoundTrip(t *testing.T) {
	reg := config.Registry{Instances: []config.Instance{{
		Name:              "work",
		Backend:           "windows-user",
		Username:          "wechatctl_work",
		EncryptedPassword: "abc",
	}}}
	data, err := toml.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	var got config.Registry
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Instances) != 1 {
		t.Fatalf("instances=%d", len(got.Instances))
	}
	inst := got.Instances[0]
	if inst.Backend != "windows-user" || inst.Username != "wechatctl_work" || inst.EncryptedPassword != "abc" {
		t.Fatalf("round trip mismatch: %+v", inst)
	}
	if inst.EffectiveBackend() != "windows-user" {
		t.Fatalf("EffectiveBackend=%q", inst.EffectiveBackend())
	}
}

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
