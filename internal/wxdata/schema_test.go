package wxdata

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestProbeSessionMissingColumn(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE SessionTable (
		username TEXT,
		unread_count INTEGER,
		summary BLOB,
		last_timestamp INTEGER
	)`)
	if err != nil {
		t.Fatal(err)
	}

	err = Probe(db, KindSession)
	if err == nil {
		t.Fatal("expected schema error")
	}
	var we *Error
	if !errors.As(err, &we) || we.Code != 6 {
		t.Fatalf("expected ErrSchema code 6, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "last_msg_type") || !strings.Contains(msg, "sqlite_master") {
		t.Fatalf("error should mention missing column and tables: %s", msg)
	}
}

func TestProbeContactMissingTable(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE contact (
		id INTEGER, username TEXT, nick_name TEXT, remark TEXT,
		alias TEXT, description TEXT, small_head_url TEXT, big_head_url TEXT,
		verify_flag INTEGER, local_type INTEGER
	)`)
	if err != nil {
		t.Fatal(err)
	}

	err = Probe(db, KindContact)
	if err == nil {
		t.Fatal("expected schema error")
	}
	var we *Error
	if !errors.As(err, &we) || we.Code != 6 {
		t.Fatalf("expected ErrSchema code 6, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "chat_room") || !strings.Contains(msg, "sqlite_master") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return db
}
