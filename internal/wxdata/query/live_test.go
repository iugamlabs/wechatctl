package query

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"github.com/star-plan/wechatctl/internal/wxdata"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

// TestLiveQuery 在已 init-data 的实例上探测 schema 并做最小查询（需本机微信数据）。
// 默认 Skip；见 README「Live 集成测试」。
func TestLiveQuery(t *testing.T) {
	name := os.Getenv("WXCTL_LIVE_INSTANCE")
	if name == "" {
		t.Skip("set WXCTL_LIVE_INSTANCE=work to run live tests")
	}

	layout, err := paths.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}

	store, err := wxdata.Open(layout, cfg, name)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.OpenDB(sessionDBKey, wxdata.KindSession); err != nil {
		t.Fatalf("session schema: %v", err)
	}
	book, _, err := LoadBook(store)
	if err != nil {
		t.Fatalf("contact schema: %v", err)
	}
	if rel := firstMessageDBRel(store.Keys); rel != "" {
		if _, err := store.OpenDB(rel, wxdata.KindMessage); err != nil {
			t.Fatalf("message schema (%s): %v", rel, err)
		}
	} else {
		t.Log("no message/message_*.db key in all_keys.json; skipping message schema probe")
	}

	items, err := ListSessions(store, book, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 1 {
		t.Fatalf("expected at most 1 session, got %d", len(items))
	}
	for _, item := range items {
		assertSessionJSONFields(t, item)
	}

	if len(items) == 0 {
		return
	}
	chat := items[0].Chat
	if chat == "" {
		chat = items[0].Username
	}
	ctx, err := ResolveChatContext(store, book, chat)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil || len(ctx.MessageTables) == 0 {
		t.Logf("no message tables for chat %q; skip history probe", chat)
		return
	}
	_, failures, err := CollectChatHistory(store, book, ctx, nil, nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatalf("history failures: %v", failures)
	}
}

func firstMessageDBRel(keyMap map[string]keys.KeyInfo) string {
	var rels []string
	for rel := range keyMap {
		if keys.MessageDBRelRE.MatchString(rel) {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	if len(rels) == 0 {
		return ""
	}
	return rels[0]
}

func assertSessionJSONFields(t *testing.T, item SessionItem) {
	t.Helper()
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"chat", "username", "is_group", "unread", "last_message",
		"msg_type", "sender", "timestamp", "time",
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Fatalf("sessions JSON missing field %q", k)
		}
	}
}
