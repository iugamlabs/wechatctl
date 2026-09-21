package keys

import (
	"os"
	"strings"
	"testing"
)

func TestParseMapsFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/proc_maps.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := ParseMaps(strings.NewReader(string(data)))

	want := map[uint64]uint64{
		0x00400000: 0xb000,
		0x0040b000: 0x1000,
		0x0040c000: 0x1000,
		0x7f000000: 0x100000,
		0x7f300000: 0x1000, // libwcdb.so
		0x7f400000: 0x1000, // libwechat.so
		0x7f500000: 0x1000, // libweixin.so
		0xaaaaaaaa: 1,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d regions want %d: %+v", len(got), len(want), got)
	}
	for _, r := range got {
		sz, ok := want[r.Start]
		if !ok {
			t.Errorf("unexpected region start=0x%x size=%d", r.Start, r.Size)
			continue
		}
		if r.Size != sz {
			t.Errorf("region 0x%x size=%d want %d", r.Start, r.Size, sz)
		}
	}
}

func TestParseMapsSkipsNoReadAndHuge(t *testing.T) {
	src := strings.Join([]string{
		"1000-2000 ---p 00000000 00:00 0",
		"2000-3000 -w-p 00000000 00:00 0",
		"0000-0000 r--p 00000000 00:00 0",
		// 512MiB
		"0-20000000 rw-p 00000000 00:00 0",
		// exactly 500MiB
		"0-1f400000 rw-p 00000000 00:00 0",
		"1000-2000 r-xp 00000000 00:00 0",
	}, "\n")
	got := ParseMaps(strings.NewReader(src))
	if len(got) != 1 || got[0].Start != 0x1000 || got[0].Size != 0x1000 {
		t.Fatalf("got %+v", got)
	}
}

func TestEnvironHasInstance(t *testing.T) {
	env := []byte("HOME=/tmp\x00WXCTL_INSTANCE=work\x00PATH=/bin\x00")
	if !EnvironHasInstance(env, "work") {
		t.Fatal("work should match")
	}
	if EnvironHasInstance(env, "work2") {
		t.Fatal("work2 must not match prefix work")
	}
	if EnvironHasInstance(env, "wor") {
		t.Fatal("prefix must not match")
	}
	if EnvironHasInstance(nil, "work") {
		t.Fatal("empty environ is fail-closed")
	}
	if EnvironHasInstance([]byte("WXCTL_INSTANCE=work2"), "work") {
		t.Fatal("missing NUL still exact-match; work2 != work")
	}
	// 无 NUL 时整段等于 needle 才算（bytes.Split 对无分隔符返回原切片）
	if !EnvironHasInstance([]byte("WXCTL_INSTANCE=work"), "work") {
		t.Fatal("single token without trailing NUL should match")
	}
}

func TestCapEffHasPtrace(t *testing.T) {
	if !CapEffHasPtrace([]byte("Name:\twxctl\nCapEff:\t0000000000080000\n")) {
		t.Fatal("bit 19 should be ptrace")
	}
	if CapEffHasPtrace([]byte("CapEff:\t0000000000000000\n")) {
		t.Fatal("zero caps")
	}
	if CapEffHasPtrace([]byte("Name:\tfoo\n")) {
		t.Fatal("missing CapEff")
	}
}
