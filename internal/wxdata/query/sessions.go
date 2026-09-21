package query

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/star-plan/wechatctl/internal/wxdata"
)

const sessionDBKey = "session/session.db"

// SessionItem 是 sessions/unread JSON 元素。
type SessionItem struct {
	Chat        string `json:"chat"`
	Username    string `json:"username"`
	IsGroup     bool   `json:"is_group"`
	Unread      int    `json:"unread"`
	LastMessage string `json:"last_message"`
	MsgType     string `json:"msg_type"`
	Sender      string `json:"sender"`
	Timestamp   int64  `json:"timestamp"`
	Time        string `json:"time"`
}

// NewMessagesFirstResult 首次 new-messages JSON。
type NewMessagesFirstResult struct {
	FirstCall   bool          `json:"first_call"`
	UnreadCount int           `json:"unread_count"`
	Messages    []SessionItem `json:"messages"`
}

// NewMessageItem 是后续 new-messages 消息项。
type NewMessageItem struct {
	Chat        string `json:"chat"`
	Username    string `json:"username"`
	IsGroup     bool   `json:"is_group"`
	LastMessage string `json:"last_message"`
	MsgType     string `json:"msg_type"`
	Sender      string `json:"sender"`
	Time        string `json:"time"`
	Timestamp   int64  `json:"timestamp"`
}

// NewMessagesResult 后续 new-messages JSON。
type NewMessagesResult struct {
	FirstCall bool             `json:"first_call"`
	NewCount  int              `json:"new_count"`
	Messages  []NewMessageItem `json:"messages"`
}

type sessionRow struct {
	username   string
	unread     int
	summary    interface{}
	timestamp  int64
	msgType    int64
	sender     string
	senderName string
}

// ListSessions 查询最近会话。
func ListSessions(store *wxdata.Store, book *Book, limit int) ([]SessionItem, error) {
	db, err := store.OpenDB(sessionDBKey, wxdata.KindSession)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT username, unread_count, summary, last_timestamp,
		       last_msg_type, last_msg_sender, last_sender_display_name
		FROM SessionTable
		WHERE last_timestamp > 0
		ORDER BY last_timestamp DESC
		LIMIT ?`
	rows, err := db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionItems(rows, book, sessionTimeLayout)
}

// ListUnread 查询未读会话。
func ListUnread(store *wxdata.Store, book *Book, limit int) ([]SessionItem, error) {
	db, err := store.OpenDB(sessionDBKey, wxdata.KindSession)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT username, unread_count, summary, last_timestamp,
		       last_msg_type, last_msg_sender, last_sender_display_name
		FROM SessionTable
		WHERE unread_count > 0
		ORDER BY last_timestamp DESC
		LIMIT ?`
	rows, err := db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionItems(rows, book, sessionTimeLayout)
}

const sessionTimeLayout = "01-02 15:04"

func scanSessionItems(rows *sql.Rows, book *Book, timeLayout string) ([]SessionItem, error) {
	var out []SessionItem
	for rows.Next() {
		r, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rowToSessionItem(r, book, timeLayout))
	}
	return out, rows.Err()
}

func scanSessionRow(rows *sql.Rows) (sessionRow, error) {
	var username, sender, senderName sql.NullString
	var unread sql.NullInt64
	var summary interface{}
	var ts sql.NullInt64
	var msgType sql.NullInt64
	if err := rows.Scan(&username, &unread, &summary, &ts, &msgType, &sender, &senderName); err != nil {
		return sessionRow{}, err
	}
	return sessionRow{
		username:   username.String,
		unread:     int(unread.Int64),
		summary:    summary,
		timestamp:  ts.Int64,
		msgType:    msgType.Int64,
		sender:     sender.String,
		senderName: senderName.String,
	}, nil
}

func rowToSessionItem(r sessionRow, book *Book, timeLayout string) SessionItem {
	isGroup := strings.Contains(r.username, "@chatroom")
	display := book.DisplayName(r.username)
	lastMsg := FormatSessionSummary(r.summary)
	senderDisplay := ""
	if isGroup && r.sender != "" {
		senderDisplay = book.DisplayName(r.sender)
		if senderDisplay == r.sender && r.senderName != "" {
			senderDisplay = r.senderName
		}
	}
	t := time.Unix(r.timestamp, 0)
	return SessionItem{
		Chat:        display,
		Username:    r.username,
		IsGroup:     isGroup,
		Unread:      r.unread,
		LastMessage: lastMsg,
		MsgType:     FormatMsgType(r.msgType),
		Sender:      senderDisplay,
		Timestamp:   r.timestamp,
		Time:        t.Format(timeLayout),
	}
}

// ListAllSessions 读取全部会话行（new-messages）。
func ListAllSessions(store *wxdata.Store) ([]sessionRow, error) {
	db, err := store.OpenDB(sessionDBKey, wxdata.KindSession)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT username, unread_count, summary, last_timestamp,
		       last_msg_type, last_msg_sender, last_sender_display_name
		FROM SessionTable
		WHERE last_timestamp > 0
		ORDER BY last_timestamp DESC`
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		r, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunNewMessages 执行 new-messages 逻辑并更新游标。
func RunNewMessages(store *wxdata.Store, book *Book, reset bool) (interface{}, error) {
	if reset {
		if err := store.RemoveLastCheck(); err != nil {
			return nil, err
		}
	}
	rows, err := ListAllSessions(store)
	if err != nil {
		return nil, err
	}
	curr := make(map[string]sessionRow, len(rows))
	for _, r := range rows {
		curr[r.username] = r
	}

	lastState, err := store.LoadLastCheck()
	if err != nil {
		return nil, err
	}

	newState := make(map[string]int64, len(curr))
	for u, r := range curr {
		newState[u] = r.timestamp
	}

	if len(lastState) == 0 {
		var unreadMsgs []SessionItem
		for _, r := range curr {
			if r.unread > 0 {
				unreadMsgs = append(unreadMsgs, rowToSessionItem(r, book, "15:04"))
			}
		}
		if err := store.SaveLastCheck(newState); err != nil {
			return nil, err
		}
		if unreadMsgs == nil {
			unreadMsgs = []SessionItem{}
		}
		return NewMessagesFirstResult{
			FirstCall:   true,
			UnreadCount: len(unreadMsgs),
			Messages:    unreadMsgs,
		}, nil
	}

	var newMsgs []NewMessageItem
	for username, r := range curr {
		prev := lastState[username]
		if r.timestamp > prev {
			isGroup := strings.Contains(username, "@chatroom")
			lastMsg := FormatSessionSummary(r.summary)
			senderDisplay := ""
			if isGroup && r.sender != "" {
				senderDisplay = book.DisplayName(r.sender)
				if senderDisplay == r.sender && r.senderName != "" {
					senderDisplay = r.senderName
				}
			}
			t := time.Unix(r.timestamp, 0)
			newMsgs = append(newMsgs, NewMessageItem{
				Chat:        book.DisplayName(username),
				Username:    username,
				IsGroup:     isGroup,
				LastMessage: lastMsg,
				MsgType:     FormatMsgType(r.msgType),
				Sender:      senderDisplay,
				Time:        t.Format("15:04:05"),
				Timestamp:   r.timestamp,
			})
		}
	}
	sort.Slice(newMsgs, func(i, j int) bool {
		return newMsgs[i].Timestamp < newMsgs[j].Timestamp
	})

	if err := store.SaveLastCheck(newState); err != nil {
		return nil, err
	}
	if newMsgs == nil {
		newMsgs = []NewMessageItem{}
	}
	return NewMessagesResult{
		FirstCall: false,
		NewCount:  len(newMsgs),
		Messages:  newMsgs,
	}, nil
}
