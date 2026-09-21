package wxdata

import (
	"database/sql"
	"fmt"
	"strings"
)

// Kind 是 schema 探测的数据库类别。
type Kind string

const (
	KindSession Kind = "session"
	KindContact Kind = "contact"
	KindMessage Kind = "message"
)

type tableSpec struct {
	name    string
	columns []string
}

var (
	sessionTables = []tableSpec{
		{name: "SessionTable", columns: []string{
			"username", "unread_count", "summary", "last_timestamp",
			"last_msg_type", "last_msg_sender", "last_sender_display_name",
		}},
	}
	contactTables = []tableSpec{
		{name: "contact", columns: []string{
			"id", "username", "nick_name", "remark", "alias", "description",
			"small_head_url", "big_head_url", "verify_flag", "local_type",
		}},
		{name: "chat_room", columns: []string{"id", "owner"}},
		{name: "chatroom_member", columns: []string{"room_id", "member_id"}},
	}
	messageRequired = tableSpec{name: "Name2Id", columns: []string{"user_name"}}
	messageMsgCols  = []string{
		"local_id", "local_type", "create_time", "real_sender_id",
		"message_content", "WCDB_CT_message_content",
	}
)

// Probe 检查解密后的 sqlite 是否与 wechat-cli 假设一致。
func Probe(db *sql.DB, kind Kind) error {
	var specs []tableSpec
	switch kind {
	case KindSession:
		specs = sessionTables
	case KindContact:
		specs = contactTables
	case KindMessage:
		specs = nil
	default:
		return fmt.Errorf("unknown schema kind %q: %w", kind, ErrSchema)
	}

	var missing []string
	for _, spec := range specs {
		if !tableExists(db, spec.name) {
			missing = append(missing, "missing table "+spec.name)
			continue
		}
		miss := missingColumns(db, spec.name, spec.columns)
		if len(miss) > 0 {
			missing = append(missing, fmt.Sprintf("table %s missing columns: %s", spec.name, strings.Join(miss, ", ")))
		}
	}

	if kind == KindMessage {
		if !tableExists(db, messageRequired.name) {
			missing = append(missing, "missing table "+messageRequired.name)
		} else {
			miss := missingColumns(db, messageRequired.name, messageRequired.columns)
			if len(miss) > 0 {
				missing = append(missing, fmt.Sprintf("table %s missing columns: %s", messageRequired.name, strings.Join(miss, ", ")))
			}
		}
		msgTables, err := listMsgTables(db)
		if err != nil {
			return fmt.Errorf("Linux schema inconsistent with wechat-cli assumptions: %w: %w", err, ErrSchema)
		}
		for _, tbl := range msgTables {
			miss := missingColumns(db, tbl, messageMsgCols)
			if len(miss) > 0 {
				missing = append(missing, fmt.Sprintf("table %s missing columns: %s", tbl, strings.Join(miss, ", ")))
			}
		}
	}

	if len(missing) == 0 {
		return nil
	}

	tables, err := listUserTables(db, 30)
	if err != nil {
		return fmt.Errorf("Linux schema inconsistent with wechat-cli assumptions: %s: %w", strings.Join(missing, "; "), ErrSchema)
	}
	return fmt.Errorf(
		"Linux schema inconsistent with wechat-cli assumptions: %s; sqlite_master tables (%d): %s: %w",
		strings.Join(missing, "; "),
		len(tables),
		strings.Join(tables, ", "),
		ErrSchema,
	)
}

func missingColumns(db *sql.DB, table string, required []string) []string {
	cols, err := tableColumns(db, table)
	if err != nil {
		return required
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[strings.ToLower(c)] = true
	}
	var miss []string
	for _, want := range required {
		if !set[strings.ToLower(want)] {
			miss = append(miss, want)
		}
	}
	return miss
}

func tableExists(db *sql.DB, name string) bool {
	var n string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
		name,
	).Scan(&n)
	return err == nil
}

func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + quoteIdent(table) + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func listUserTables(db *sql.DB, limit int) ([]string, error) {
	rows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func listMsgTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'Msg_%'",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if !isSafeMsgTable(n) {
			continue
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isSafeMsgTable(name string) bool {
	if len(name) != 36 || !strings.HasPrefix(name, "Msg_") {
		return false
	}
	for _, c := range name[4:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
