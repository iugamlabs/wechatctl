package query

import (
	"testing"
	"time"
)

func TestParseTimeRangeEndOfDay(t *testing.T) {
	endTS, err := ParseTimeValue("2026-04-01", "end_time", true)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 23, 59, 59, 0, time.Local).Unix()
	if *endTS != want {
		t.Fatalf("end of day: got %d want %d", *endTS, want)
	}
}

func TestParseTimeValueDateTimeMinute(t *testing.T) {
	ts, err := ParseTimeValue("2026-04-01 15:30", "start_time", false)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 15, 30, 0, 0, time.Local).Unix()
	if *ts != want {
		t.Fatalf("got %d want %d", *ts, want)
	}
}

func TestParseTimeValueDateTimeSeconds(t *testing.T) {
	ts, err := ParseTimeValue("2026-04-01 15:30:45", "start_time", false)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 15, 30, 45, 0, time.Local).Unix()
	if *ts != want {
		t.Fatalf("got %d want %d", *ts, want)
	}
}

func TestParseTimeRangeStartAfterEnd(t *testing.T) {
	_, _, err := ParseTimeRange("2026-04-02", "2026-04-01")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePaginationSearchLimit(t *testing.T) {
	if err := ValidatePagination(501, 0, 500); err == nil {
		t.Fatal("expected limit max error")
	}
	if err := ValidatePagination(50, 0, 0); err != nil {
		t.Fatalf("history unlimited: %v", err)
	}
	if err := ValidatePagination(0, 0, 500); err == nil {
		t.Fatal("expected limit > 0 error")
	}
	if err := ValidatePagination(10, -1, 500); err == nil {
		t.Fatal("expected offset error")
	}
}
