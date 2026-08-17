package backend

import "testing"

func TestUsernameForInstanceShort(t *testing.T) {
	got := UsernameForInstance("work")
	if got != "wechatctl_work" {
		t.Fatalf("got %q, want wechatctl_work", got)
	}
}

func TestUsernameForInstanceLong(t *testing.T) {
	got := UsernameForInstance("very-long-instance")
	if len(got) > maxSAMName {
		t.Fatalf("username %q exceeds SAM limit %d", got, maxSAMName)
	}
	if !IsManagedUsername(got) {
		t.Fatalf("generated username %q should be treated as managed", got)
	}
	again := UsernameForInstance("very-long-instance")
	if again != got {
		t.Fatalf("username should be deterministic: %q vs %q", got, again)
	}
}

func TestIsManagedUsername(t *testing.T) {
	if !IsManagedUsername("wechatctl_work") {
		t.Fatal("wechatctl_work should be managed")
	}
	if !IsManagedUsername("wx_abcd1234") {
		t.Fatal("wx_ prefix should be managed")
	}
	if IsManagedUsername("Administrator") {
		t.Fatal("Administrator must not be treated as managed")
	}
}
