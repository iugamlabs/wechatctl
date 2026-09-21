package query

import (
	"fmt"
	"strings"
	"time"
)

// ParseTimeValue 解析时间字符串；仅日期且 isEnd 时取当天 23:59:59（本地时区）。
func ParseTimeValue(value, fieldName string, isEnd bool) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	layouts := []struct {
		layout   string
		dateOnly bool
	}{
		{"2006-01-02 15:04:05", false},
		{"2006-01-02 15:04", false},
		{"2006-01-02", true},
	}
	for _, item := range layouts {
		t, err := time.ParseInLocation(item.layout, value, time.Local)
		if err != nil {
			continue
		}
		if item.dateOnly && isEnd {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
		}
		ts := t.Unix()
		return &ts, nil
	}
	return nil, fmt.Errorf("%s invalid format: %s (want YYYY-MM-DD / YYYY-MM-DD HH:MM / YYYY-MM-DD HH:MM:SS)", fieldName, value)
}

// ParseTimeRange 解析起止时间；start>end 返回错误。
func ParseTimeRange(startTime, endTime string) (startTS, endTS *int64, err error) {
	startTS, err = ParseTimeValue(startTime, "start_time", false)
	if err != nil {
		return nil, nil, err
	}
	endTS, err = ParseTimeValue(endTime, "end_time", true)
	if err != nil {
		return nil, nil, err
	}
	if startTS != nil && endTS != nil && *startTS > *endTS {
		return nil, nil, fmt.Errorf("start_time must not be after end_time")
	}
	return startTS, endTS, nil
}

// ValidatePagination 校验 limit/offset；limitMax==0 表示无上限（history/chat-export）。
func ValidatePagination(limit, offset int, limitMax int) error {
	if limit <= 0 {
		return fmt.Errorf("limit must be > 0")
	}
	if limitMax > 0 && limit > limitMax {
		return fmt.Errorf("limit must be <= %d", limitMax)
	}
	if offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	return nil
}
