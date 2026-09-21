package query

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/star-plan/wechatctl/internal/wxdata"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

const historyQueryBatchSize = 500

var safeMsgTableRE = regexp.MustCompile(`^Msg_[0-9a-f]{32}$`)

// MsgTypeNames 是 --type 合法枚举。
var MsgTypeNames = []string{
	"text", "image", "voice", "video", "sticker", "location", "link", "file", "call", "system",
}

// MsgTypeFilters 对齐 messages.py MSG_TYPE_FILTERS。
var MsgTypeFilters = map[string][]int64{
	"text":     {1},
	"image":    {3},
	"voice":    {34},
	"video":    {43},
	"sticker":  {47},
	"location": {48},
	"link":     {49},
	"file":     {49, 6},
	"call":     {50},
	"system":   {10000},
}

// Message 是 history JSON 单条消息。
type Message struct {
	LocalID   int64  `json:"local_id"`
	Timestamp int64  `json:"timestamp"`
	Time      string `json:"time"`
	Sender    string `json:"sender"`
	Type      string `json:"type"`
	Text      string `json:"text"`
}

// ToTextLine 生成 text/chat-export 行（不含外围方括号时间重复 — 时间已在 Time 字段）。
func (m Message) ToTextLine() string {
	line := "[" + m.Time + "]"
	if m.Sender != "" {
		line += " " + m.Sender + ": " + m.Text
	} else {
		line += " " + m.Text
	}
	return line
}

// HistoryResult 是 history 命令 JSON。
type HistoryResult struct {
	Chat      string    `json:"chat"`
	Username  string    `json:"username"`
	IsGroup   bool      `json:"is_group"`
	Count     int       `json:"count"`
	Offset    int       `json:"offset"`
	Limit     int       `json:"limit"`
	StartTime *string   `json:"start_time"`
	EndTime   *string   `json:"end_time"`
	Type      *string   `json:"type"`
	Messages  []Message `json:"messages"`
	Failures  *[]string `json:"failures"`
}

// SearchHit 是 search JSON 单条结果。
type SearchHit struct {
	Timestamp int64  `json:"timestamp"`
	Time      string `json:"time"`
	Chat      string `json:"chat"`
	Sender    string `json:"sender"`
	Type      string `json:"type"`
	Text      string `json:"text"`
}

// ToSearchTextLine 是 search text 模式行格式。
func (h SearchHit) ToSearchTextLine() string {
	line := "[" + h.Time + "] [" + h.Chat + "]"
	if h.Sender != "" {
		line += " " + h.Sender + ": " + h.Text
	} else {
		line += " " + h.Text
	}
	return line
}

// SearchResult 是 search 命令 JSON。
type SearchResult struct {
	Scope     string      `json:"scope"`
	Keyword   string      `json:"keyword"`
	Count     int         `json:"count"`
	Offset    int         `json:"offset"`
	Limit     int         `json:"limit"`
	StartTime *string     `json:"start_time"`
	EndTime   *string     `json:"end_time"`
	Type      *string     `json:"type"`
	Results   []SearchHit `json:"results"`
	Failures  *[]string   `json:"failures"`
}

// ChatContext 是单聊查询上下文。
type ChatContext struct {
	Query          string
	Username       string
	DisplayName    string
	IsGroup        bool
	MessageTables  []messageTableRef
}

type messageTableRef struct {
	RelKey    string
	TableName string
	MaxTime   int64
}

type tableQueryCtx struct {
	Username    string
	DisplayName string
	IsGroup     bool
	RelKey      string
	TableName   string
}

// MsgTableName 返回 Msg_<md5(username)> 表名。
func MsgTableName(username string) string {
	sum := md5.Sum([]byte(username))
	return "Msg_" + hex.EncodeToString(sum[:])
}

func isSafeMsgTableName(name string) bool {
	return safeMsgTableRE.MatchString(name)
}

// ParseMsgTypeFilter 解析 --type；空字符串表示不过滤。
func ParseMsgTypeFilter(name string) ([]int64, error) {
	if name == "" {
		return nil, nil
	}
	f, ok := MsgTypeFilters[name]
	if !ok {
		return nil, fmt.Errorf("unknown message type %q", name)
	}
	return f, nil
}

func sortedMsgDBKeys(keyMap map[string]keys.KeyInfo) []string {
	var rels []string
	for k := range keyMap {
		k = filepath.ToSlash(k)
		if keys.MessageDBRelRE.MatchString(k) {
			rels = append(rels, k)
		}
	}
	sort.Strings(rels)
	return rels
}

// ResolveChatContext 解析聊天并发现消息分表。
func ResolveChatContext(store *wxdata.Store, book *Book, chatName string) (*ChatContext, error) {
	username := book.ResolveUsername(chatName)
	if username == "" {
		return nil, nil
	}
	tables, err := findMsgTablesForUser(store, username, sortedMsgDBKeys(store.Keys))
	if err != nil {
		return nil, err
	}
	return &ChatContext{
		Query:         chatName,
		Username:      username,
		DisplayName:   book.DisplayName(username),
		IsGroup:       strings.Contains(username, "@chatroom"),
		MessageTables: tables,
	}, nil
}

// ResolveChatContexts 多聊解析；返回失败名列表。
func ResolveChatContexts(store *wxdata.Store, book *Book, chatNames []string) (resolved []*ChatContext, unresolved []string, missingTables []string) {
	seen := make(map[string]bool)
	for _, chatName := range chatNames {
		name := strings.TrimSpace(chatName)
		if name == "" {
			unresolved = append(unresolved, "(空)")
			continue
		}
		ctx, err := ResolveChatContext(store, book, name)
		if err != nil {
			unresolved = append(unresolved, name)
			continue
		}
		if ctx == nil {
			unresolved = append(unresolved, name)
			continue
		}
		if len(ctx.MessageTables) == 0 {
			missingTables = append(missingTables, ctx.DisplayName)
			continue
		}
		if seen[ctx.Username] {
			continue
		}
		seen[ctx.Username] = true
		resolved = append(resolved, ctx)
	}
	return resolved, unresolved, missingTables
}

func findMsgTablesForUser(store *wxdata.Store, username string, msgKeys []string) ([]messageTableRef, error) {
	tableName := MsgTableName(username)
	if !isSafeMsgTableName(tableName) {
		return nil, nil
	}
	var matches []messageTableRef
	for _, rel := range msgKeys {
		db, err := store.OpenDB(rel, wxdata.KindMessage)
		if err != nil {
			continue
		}
		var one int
		err = db.QueryRow("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&one)
		if err != nil {
			continue
		}
		var maxCT sql.NullInt64
		_ = db.QueryRow("SELECT MAX(create_time) FROM [" + tableName + "]").Scan(&maxCT)
		matches = append(matches, messageTableRef{
			RelKey:    rel,
			TableName: tableName,
			MaxTime:   maxCT.Int64,
		})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].MaxTime > matches[j].MaxTime })
	return matches, nil
}

func iterTableContexts(ctx *ChatContext) []tableQueryCtx {
	var out []tableQueryCtx
	for _, t := range ctx.MessageTables {
		out = append(out, tableQueryCtx{
			Username:    ctx.Username,
			DisplayName: ctx.DisplayName,
			IsGroup:     ctx.IsGroup,
			RelKey:      t.RelKey,
			TableName:   t.TableName,
		})
	}
	return out
}

type messageFilters struct {
	clauses []string
	params  []interface{}
}

func buildMessageFilters(startTS, endTS *int64, keyword string, typeFilter []int64) messageFilters {
	var f messageFilters
	if startTS != nil {
		f.clauses = append(f.clauses, "create_time >= ?")
		f.params = append(f.params, *startTS)
	}
	if endTS != nil {
		f.clauses = append(f.clauses, "create_time <= ?")
		f.params = append(f.params, *endTS)
	}
	if keyword != "" {
		f.clauses = append(f.clauses, "message_content LIKE ?")
		f.params = append(f.params, "%"+keyword+"%")
	}
	if typeFilter != nil {
		f.clauses = append(f.clauses, "(local_type & 0xFFFFFFFF) = ?")
		f.params = append(f.params, typeFilter[0])
		if len(typeFilter) > 1 {
			f.clauses = append(f.clauses, "((local_type >> 32) & 0xFFFFFFFF) = ?")
			f.params = append(f.params, typeFilter[1])
		}
	}
	return f
}

func queryMessageRows(db *sql.DB, tableName string, f messageFilters, limit, offset int) ([][]interface{}, error) {
	if !isSafeMsgTableName(tableName) {
		return nil, fmt.Errorf("invalid message table %q", tableName)
	}
	where := ""
	if len(f.clauses) > 0 {
		where = "WHERE " + strings.Join(f.clauses, " AND ")
	}
	sqlStr := "SELECT local_id, local_type, create_time, real_sender_id, message_content, WCDB_CT_message_content FROM [" +
		tableName + "] " + where + " ORDER BY create_time DESC LIMIT ? OFFSET ?"
	params := append(f.params, limit, offset)
	rows, err := db.Query(sqlStr, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]interface{}
	for rows.Next() {
		var localID, localType, createTime, realSenderID sql.NullInt64
		var content []byte
		var ct sql.NullInt64
		if err := rows.Scan(&localID, &localType, &createTime, &realSenderID, &content, &ct); err != nil {
			return nil, err
		}
		out = append(out, []interface{}{
			localID.Int64, localType.Int64, createTime.Int64, realSenderID.Int64, content, int(ct.Int64),
		})
	}
	return out, rows.Err()
}

func loadName2ID(db *sql.DB) map[int64]string {
	out := make(map[int64]string)
	rows, err := db.Query("SELECT rowid, user_name FROM Name2Id")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullInt64
		var uname sql.NullString
		if err := rows.Scan(&id, &uname); err != nil {
			continue
		}
		if uname.String != "" {
			out[id.Int64] = uname.String
		}
	}
	return out
}

func buildHistoryMessage(row []interface{}, tctx tableQueryCtx, book *Book, dbDir string, idToUsername map[int64]string) (Message, error) {
	localID := row[0].(int64)
	localType := row[1].(int64)
	createTime := row[2].(int64)
	realSenderID := row[3].(int64)
	content := row[4].([]byte)
	ct := row[5].(int)

	text, ok := DecompressContent(content, ct)
	if !ok {
		text = HistoryContentPlaceholder
	}
	senderFromContent, body := FormatMessageText(localID, localType, text, tctx.IsGroup)
	sender := ResolveSenderLabel(realSenderID, senderFromContent, tctx.IsGroup, tctx.Username, tctx.DisplayName, idToUsername, book, dbDir)
	return Message{
		LocalID:   localID,
		Timestamp: createTime,
		Time:      FormatMessageTime(createTime),
		Sender:    sender,
		Type:      FormatMsgType(localType),
		Text:      body,
	}, nil
}

func buildSearchHit(row []interface{}, tctx tableQueryCtx, book *Book, dbDir string, idToUsername map[int64]string) (*SearchHit, error) {
	localID := row[0].(int64)
	localType := row[1].(int64)
	createTime := row[2].(int64)
	realSenderID := row[3].(int64)
	content := row[4].([]byte)
	ct := row[5].(int)

	text, ok := DecompressContent(content, ct)
	if !ok {
		return nil, nil
	}
	senderFromContent, body := FormatMessageText(localID, localType, text, tctx.IsGroup)
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	sender := ResolveSenderLabel(realSenderID, senderFromContent, tctx.IsGroup, tctx.Username, tctx.DisplayName, idToUsername, book, dbDir)
	return &SearchHit{
		Timestamp: createTime,
		Time:      FormatMessageTime(createTime),
		Chat:      tctx.DisplayName,
		Sender:    sender,
		Type:      FormatMsgType(localType),
		Text:      body,
	}, nil
}

type rankedMessage struct {
	ts  int64
	msg Message
}

type rankedSearch struct {
	ts  int64
	hit SearchHit
}

func pageRankedMessages(entries []rankedMessage, limit, offset int) []Message {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts > entries[j].ts })
	if offset > len(entries) {
		return nil
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	slice := entries[offset:end]
	sort.Slice(slice, func(i, j int) bool { return slice[i].ts < slice[j].ts })
	out := make([]Message, len(slice))
	for i, e := range slice {
		out[i] = e.msg
	}
	return out
}

func pageRankedSearch(entries []rankedSearch, limit, offset int) []SearchHit {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts > entries[j].ts })
	if offset > len(entries) {
		return nil
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	slice := entries[offset:end]
	sort.Slice(slice, func(i, j int) bool { return slice[i].ts < slice[j].ts })
	out := make([]SearchHit, len(slice))
	for i, e := range slice {
		out[i] = e.hit
	}
	return out
}

func candidatePageSize(limit, offset int) int {
	return limit + offset
}

// CollectChatHistory 查询单聊历史（含多表合并分页）。
func CollectChatHistory(store *wxdata.Store, book *Book, ctx *ChatContext, startTS, endTS *int64, limit, offset int, typeFilter []int64) ([]Message, []string) {
	candidateLimit := candidatePageSize(limit, offset)
	batchSize := candidateLimit
	if batchSize > historyQueryBatchSize {
		batchSize = historyQueryBatchSize
	}
	filters := buildMessageFilters(startTS, endTS, "", typeFilter)

	var collected []rankedMessage
	var failures []string
	dbDir := store.DBDir

	for _, tctx := range iterTableContexts(ctx) {
		db, err := store.OpenDB(tctx.RelKey, wxdata.KindMessage)
		if err != nil {
			failures = append(failures, tctx.RelKey+": "+err.Error())
			continue
		}
		idToUsername := loadName2ID(db)
		fetchOffset := 0
		before := len(collected)
		for len(collected)-before < candidateLimit {
			rows, err := queryMessageRows(db, tctx.TableName, filters, batchSize, fetchOffset)
			if err != nil {
				failures = append(failures, tctx.RelKey+": "+err.Error())
				break
			}
			if len(rows) == 0 {
				break
			}
			fetchOffset += len(rows)
			for _, row := range rows {
				msg, err := buildHistoryMessage(row, tctx, book, dbDir, idToUsername)
				if err != nil {
					failures = append(failures, fmt.Sprintf("local_id=%v: %v", row[0], err))
					continue
				}
				collected = append(collected, rankedMessage{ts: msg.Timestamp, msg: msg})
				if len(collected)-before >= candidateLimit {
					break
				}
			}
			if len(rows) < batchSize {
				break
			}
		}
	}
	return pageRankedMessages(collected, limit, offset), failures
}

func collectSearchFromDB(db *sql.DB, contexts []tableQueryCtx, book *Book, dbDir string, keyword string, startTS, endTS *int64, candidateLimit int, typeFilter []int64) ([]rankedSearch, []string) {
	var collected []rankedSearch
	var failures []string
	filters := buildMessageFilters(startTS, endTS, keyword, typeFilter)
	idToUsername := loadName2ID(db)
	batchSize := candidateLimit

	for _, tctx := range contexts {
		fetchOffset := 0
		before := len(collected)
		for len(collected)-before < candidateLimit {
			rows, err := queryMessageRows(db, tctx.TableName, filters, batchSize, fetchOffset)
			if err != nil {
				failures = append(failures, tctx.DisplayName+": "+err.Error())
				break
			}
			if len(rows) == 0 {
				break
			}
			fetchOffset += len(rows)
			for _, row := range rows {
				hit, err := buildSearchHit(row, tctx, book, dbDir, idToUsername)
				if err != nil {
					failures = append(failures, fmt.Sprintf("local_id=%v: %v", row[0], err))
					continue
				}
				if hit == nil {
					continue
				}
				collected = append(collected, rankedSearch{ts: hit.Timestamp, hit: *hit})
				if len(collected)-before >= candidateLimit {
					break
				}
			}
			if len(rows) < batchSize {
				break
			}
		}
	}
	return collected, failures
}

// CollectChatSearch 在单聊（可多表）内搜索；返回候选集（分页由 PageSearchHits 完成）。
func CollectChatSearch(store *wxdata.Store, book *Book, ctx *ChatContext, keyword string, startTS, endTS *int64, candidateLimit int, typeFilter []int64) ([]SearchHit, []string) {
	byDB := make(map[string][]tableQueryCtx)
	for _, tctx := range iterTableContexts(ctx) {
		byDB[tctx.RelKey] = append(byDB[tctx.RelKey], tctx)
	}
	var collected []rankedSearch
	var failures []string
	for rel, contexts := range byDB {
		db, err := store.OpenDB(rel, wxdata.KindMessage)
		if err != nil {
			for _, c := range contexts {
				failures = append(failures, c.DisplayName+": "+err.Error())
			}
			continue
		}
		entries, f := collectSearchFromDB(db, contexts, book, store.DBDir, keyword, startTS, endTS, candidateLimit, typeFilter)
		collected = append(collected, entries...)
		failures = append(failures, f...)
	}
	return rankedSearchToHits(collected), failures
}

func loadSearchContextsFromDB(db *sql.DB, book *Book) []tableQueryCtx {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'Msg_%'")
	if err != nil {
		return nil
	}
	defer rows.Close()
	tableToUser := make(map[string]string)
	nameRows, err := db.Query("SELECT user_name FROM Name2Id")
	if err == nil {
		defer nameRows.Close()
		for nameRows.Next() {
			var uname sql.NullString
			if err := nameRows.Scan(&uname); err != nil || uname.String == "" {
				continue
			}
			tableToUser[MsgTableName(uname.String)] = uname.String
		}
	}
	var contexts []tableQueryCtx
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		if !isSafeMsgTableName(tableName) {
			continue
		}
		username := tableToUser[tableName]
		display := tableName
		if username != "" {
			display = book.DisplayName(username)
		}
		contexts = append(contexts, tableQueryCtx{
			Username:    username,
			DisplayName: display,
			IsGroup:     strings.Contains(username, "@chatroom"),
			TableName:   tableName,
		})
	}
	return contexts
}

// SearchAllMessages 全局搜索所有 message 分库。
func SearchAllMessages(store *wxdata.Store, book *Book, keyword string, startTS, endTS *int64, candidateLimit int, typeFilter []int64) ([]SearchHit, []string) {
	var collected []rankedSearch
	var failures []string
	for _, rel := range sortedMsgDBKeys(store.Keys) {
		db, err := store.OpenDB(rel, wxdata.KindMessage)
		if err != nil {
			failures = append(failures, rel+": "+err.Error())
			continue
		}
		contexts := loadSearchContextsFromDB(db, book)
		for i := range contexts {
			contexts[i].RelKey = rel
		}
		entries, f := collectSearchFromDB(db, contexts, book, store.DBDir, keyword, startTS, endTS, candidateLimit, typeFilter)
		collected = append(collected, entries...)
		failures = append(failures, f...)
	}
	return rankedSearchToHits(collected), failures
}

func rankedSearchToHits(entries []rankedSearch) []SearchHit {
	hits := make([]SearchHit, len(entries))
	for i, e := range entries {
		hits[i] = e.hit
	}
	return hits
}

// PageSearchHits 多表/多库合并后按时间分页。
func PageSearchHits(hits []SearchHit, limit, offset int) []SearchHit {
	var entries []rankedSearch
	for _, h := range hits {
		entries = append(entries, rankedSearch{ts: h.Timestamp, hit: h})
	}
	return pageRankedSearch(entries, limit, offset)
}

func optionalStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func failuresPtr(failures []string) *[]string {
	if len(failures) == 0 {
		return nil
	}
	return &failures
}

// BuildHistoryResult 组装 history JSON。
func BuildHistoryResult(ctx *ChatContext, messages []Message, failures []string, startTime, endTime, typeName string, limit, offset int) *HistoryResult {
	if messages == nil {
		messages = []Message{}
	}
	return &HistoryResult{
		Chat:      ctx.DisplayName,
		Username:  ctx.Username,
		IsGroup:   ctx.IsGroup,
		Count:     len(messages),
		Offset:    offset,
		Limit:     limit,
		StartTime: optionalStringPtr(startTime),
		EndTime:   optionalStringPtr(endTime),
		Type:      optionalStringPtr(typeName),
		Messages:  messages,
		Failures:  failuresPtr(failures),
	}
}
