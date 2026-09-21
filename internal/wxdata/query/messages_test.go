package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

func TestMsgTableName(t *testing.T) {
	got := MsgTableName("wxid_abc")
	want := "Msg_cdebbea2056901bfd390f03a4e892e6e"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if !isSafeMsgTableName(got) {
		t.Fatal("expected safe table name")
	}
}

func TestMsgDBKeyFilter(t *testing.T) {
	rels := sortedMsgDBKeys(map[string]keys.KeyInfo{
		"message/message_0.db":     {},
		"message/biz_message_0.db": {},
		"message/message_fts.db":   {},
	})
	if len(rels) != 1 || rels[0] != "message/message_0.db" {
		t.Fatalf("got %v", rels)
	}
}

func TestHistoryResultJSONGolden(t *testing.T) {
	res := HistoryResult{
		Chat:     "张三",
		Username: "wxid_x",
		Messages: []Message{{LocalID: 1, Timestamp: 1, Time: "2026-01-01 00:00", Type: "文本", Text: "hi"}},
		Failures: nil,
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"local_id"`) || strings.Contains(s, "LocalID") {
		t.Fatalf("bad json: %s", s)
	}
	if !strings.Contains(s, `"failures":null`) {
		t.Fatalf("expected failures null: %s", s)
	}
}

func TestSearchResultJSONGolden(t *testing.T) {
	res := SearchResult{
		Scope:    "全部消息",
		Keyword:  "x",
		Results:  []SearchHit{{Timestamp: 1, Time: "t", Chat: "c", Type: "文本", Text: "y"}},
		Failures: nil,
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, key := range []string{`"timestamp"`, `"chat"`, `"time"`, `"type"`, `"text"`} {
		if !strings.Contains(s, key) {
			t.Fatalf("missing %s in json: %s", key, s)
		}
	}
	if strings.Contains(s, "LocalID") || strings.Contains(s, "Timestamp") {
		t.Fatalf("exported field names in json: %s", s)
	}
	if !strings.Contains(s, `"failures":null`) {
		t.Fatalf("expected failures null: %s", s)
	}
}

func TestPageRankedMessages(t *testing.T) {
	entries := []rankedMessage{
		{ts: 3, msg: Message{LocalID: 3}},
		{ts: 1, msg: Message{LocalID: 1}},
		{ts: 2, msg: Message{LocalID: 2}},
	}
	out := pageRankedMessages(entries, 2, 0)
	if len(out) != 2 || out[0].LocalID != 2 || out[1].LocalID != 3 {
		t.Fatalf("got %+v", out)
	}
}
