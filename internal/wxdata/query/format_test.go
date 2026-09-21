package query

import (
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestFormatMsgType(t *testing.T) {
	if FormatMsgType(1) != "文本" {
		t.Fatalf("got %q", FormatMsgType(1))
	}
	if FormatMsgType(49) != "链接/文件" {
		t.Fatalf("got %q", FormatMsgType(49))
	}
	raw := FormatMsgType(999)
	if !strings.HasPrefix(raw, "type=") {
		t.Fatalf("got %q", raw)
	}
}

func TestSplitGroupSummary(t *testing.T) {
	in := "wxid_abc:\nhello"
	if SplitGroupSummary(in) != "hello" {
		t.Fatalf("got %q", SplitGroupSummary(in))
	}
}

func TestDecompressSessionSummaryFailure(t *testing.T) {
	got := FormatSessionSummary([]byte{0x01, 0x02, 0x03})
	if got != SessionSummaryPlaceholder {
		t.Fatalf("got %q", got)
	}
}

func TestFormatAppMessagePlaceholders(t *testing.T) {
	link := FormatAppMessageText(`<msg><appmsg><type>5</type><title>Example</title></appmsg></msg>`, 49)
	if link != "[链接] Example" {
		t.Fatalf("link: %q", link)
	}
	file := FormatAppMessageText(`<msg><appmsg><type>6</type><title>a.pdf</title></appmsg></msg>`, 49<<32|6)
	if file != "[文件] a.pdf" {
		t.Fatalf("file: %q", file)
	}
	longRef := strings.Repeat("引", 200)
	quote := FormatAppMessageText(
		`<msg><appmsg><type>57</type><title>回复标题</title><refermsg><displayname>张三</displayname><content>`+longRef+`</content></refermsg></appmsg></msg>`,
		49<<32|57,
	)
	if !strings.HasPrefix(quote, "回复标题") {
		t.Fatalf("quote title: %q", quote)
	}
	if !strings.Contains(quote, "↳") {
		t.Fatalf("expected arrow: %q", quote)
	}
	if !strings.Contains(quote, "回复 张三:") {
		t.Fatalf("expected reply prefix: %q", quote)
	}
	if !strings.HasSuffix(quote, "...") {
		t.Fatalf("expected truncation: %q", quote)
	}
	const prefix = "回复 张三: "
	idx := strings.Index(quote, prefix)
	if idx < 0 {
		t.Fatalf("missing ref prefix: %q", quote)
	}
	refPart := quote[idx+len(prefix):]
	if len([]rune(refPart)) != 163 {
		t.Fatalf("ref content runes: got %d want 163 (%q)", len([]rune(refPart)), refPart)
	}
}

func TestParseMessageContentGroup(t *testing.T) {
	s, body := ParseMessageContent("wxid_abc:\nhello", true)
	if s != "wxid_abc" || body != "hello" {
		t.Fatalf("got %q %q", s, body)
	}
}

func TestDecompressSessionSummarySuccess(t *testing.T) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := enc.EncodeAll([]byte("wxid:\nhi"), nil)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	got := FormatSessionSummary(data)
	if got != "hi" {
		t.Fatalf("got %q", got)
	}
}
