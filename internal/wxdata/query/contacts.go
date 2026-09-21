package query

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/star-plan/wechatctl/internal/wxdata"
)

const contactDBKey = "contact/contact.db"

// ContactRow 是联系人列表项。
type ContactRow struct {
	Username string `json:"username"`
	NickName string `json:"nick_name"`
	Remark   string `json:"remark"`
}

// ContactDetail 是联系人详情 JSON。
type ContactDetail struct {
	Username       string `json:"username"`
	NickName       string `json:"nick_name"`
	Remark         string `json:"remark"`
	Alias          string `json:"alias"`
	Description    string `json:"description"`
	Avatar         string `json:"avatar"`
	VerifyFlag     int64  `json:"verify_flag"`
	LocalType      int64  `json:"local_type"`
	IsGroup        bool   `json:"is_group"`
	IsSubscription bool   `json:"is_subscription"`
}

// GroupMember 是群成员项。
type GroupMember struct {
	Username    string `json:"username"`
	NickName    string `json:"nick_name"`
	Remark      string `json:"remark"`
	DisplayName string `json:"display_name"`
}

// MembersResult 是 members 命令 JSON。
type MembersResult struct {
	Group       string        `json:"group"`
	Username    string        `json:"username"`
	MemberCount int           `json:"member_count"`
	Owner       string        `json:"owner"`
	Members     []GroupMember `json:"members"`
}

// Book 缓存单次 CLI 内的联系人数据。
type Book struct {
	names map[string]string
	full  []ContactRow
}

// LoadBook 从 contact.db 加载联系人。
func LoadBook(store *wxdata.Store) (*Book, *sql.DB, error) {
	db, err := store.OpenDB(contactDBKey, wxdata.KindContact)
	if err != nil {
		return nil, nil, err
	}
	rows, err := db.Query("SELECT username, nick_name, remark FROM contact")
	if err != nil {
		return nil, db, err
	}
	defer rows.Close()

	names := make(map[string]string)
	var full []ContactRow
	for rows.Next() {
		var uname, nick, remark sql.NullString
		if err := rows.Scan(&uname, &nick, &remark); err != nil {
			return nil, db, err
		}
		u := uname.String
		n := nick.String
		r := remark.String
		display := r
		if display == "" {
			display = n
		}
		if display == "" {
			display = u
		}
		names[u] = display
		full = append(full, ContactRow{
			Username: u,
			NickName: n,
			Remark:   r,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, db, err
	}
	return &Book{names: names, full: full}, db, nil
}

// Names 返回 username -> 显示名。
func (b *Book) Names() map[string]string {
	return b.names
}

// Full 返回全部联系人行。
func (b *Book) Full() []ContactRow {
	return b.full
}

// DisplayName 返回显示名（备注>昵称>username）。
func (b *Book) DisplayName(username string) string {
	if d, ok := b.names[username]; ok {
		return d
	}
	return username
}

// ResolveUsername 模糊解析聊天名（对齐 contacts.py）。
func (b *Book) ResolveUsername(chatName string) string {
	if chatName == "" {
		return ""
	}
	if _, ok := b.names[chatName]; ok {
		return chatName
	}
	if strings.HasPrefix(chatName, "wxid_") || strings.Contains(chatName, "@chatroom") {
		return chatName
	}
	lower := strings.ToLower(chatName)
	for _, row := range b.full {
		display := b.DisplayName(row.Username)
		if strings.EqualFold(lower, display) {
			return row.Username
		}
	}
	for _, row := range b.full {
		display := b.DisplayName(row.Username)
		if strings.Contains(strings.ToLower(display), lower) {
			return row.Username
		}
	}
	return ""
}

// SelfUsername 推断己方 wxid。
func (b *Book) SelfUsername(dbDir string) string {
	if dbDir == "" {
		return ""
	}
	accountDir := filepath.Base(filepath.Dir(dbDir))
	candidates := []string{accountDir}
	re := regexp.MustCompile(`^(.+)_([0-9a-fA-F]{4,})$`)
	if m := re.FindStringSubmatch(accountDir); m != nil {
		candidates = []string{m[1], accountDir}
	}
	for _, c := range candidates {
		if c != "" {
			if _, ok := b.names[c]; ok {
				return c
			}
		}
	}
	return ""
}

// DisplayNameFor 己方显示为 me。
func (b *Book) DisplayNameFor(username, dbDir string) string {
	if username == "" {
		return ""
	}
	if username == b.SelfUsername(dbDir) {
		return "me"
	}
	return b.DisplayName(username)
}

// FilterContacts 按 query 过滤并截断 limit。
func FilterContacts(full []ContactRow, query string, limit int) []ContactRow {
	var matched []ContactRow
	if query == "" {
		matched = full
	} else {
		q := strings.ToLower(query)
		for _, c := range full {
			if strings.Contains(strings.ToLower(c.NickName), q) ||
				strings.Contains(strings.ToLower(c.Remark), q) ||
				strings.Contains(strings.ToLower(c.Username), q) {
				matched = append(matched, c)
			}
		}
	}
	if limit >= 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

// GetContactDetail 查询单个联系人详情。
func GetContactDetail(db *sql.DB, username string) (*ContactDetail, error) {
	row := db.QueryRow(
		"SELECT username, nick_name, remark, alias, description, "+
			"small_head_url, big_head_url, verify_flag, local_type "+
			"FROM contact WHERE username = ?",
		username,
	)
	var uname, nick, remark, alias, desc, smallURL, bigURL sql.NullString
	var verify, ltype sql.NullInt64
	if err := row.Scan(&uname, &nick, &remark, &alias, &desc, &smallURL, &bigURL, &verify, &ltype); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	u := uname.String
	avatar := smallURL.String
	if avatar == "" {
		avatar = bigURL.String
	}
	return &ContactDetail{
		Username:       u,
		NickName:       nick.String,
		Remark:         remark.String,
		Alias:          alias.String,
		Description:    desc.String,
		Avatar:         avatar,
		VerifyFlag:     verify.Int64,
		LocalType:      ltype.Int64,
		IsGroup:        strings.Contains(u, "@chatroom"),
		IsSubscription: strings.HasPrefix(u, "gh_"),
	}, nil
}

// GetGroupMembers 查询群成员（对齐 get_group_members）。
func GetGroupMembers(db *sql.DB, book *Book, chatroomUsername string) (MembersResult, error) {
	var roomID int64
	err := db.QueryRow("SELECT id FROM contact WHERE username = ?", chatroomUsername).Scan(&roomID)
	if err == sql.ErrNoRows {
		return MembersResult{Username: chatroomUsername, Members: []GroupMember{}}, nil
	}
	if err != nil {
		return MembersResult{}, err
	}

	var ownerUsername string
	ownerRow := db.QueryRow("SELECT owner FROM chat_room WHERE id = ?", roomID)
	var owner sql.NullString
	if err := ownerRow.Scan(&owner); err != nil && err != sql.ErrNoRows {
		return MembersResult{}, err
	}
	if owner.Valid {
		ownerUsername = owner.String
	}

	ownerDisplay := ""
	if ownerUsername != "" {
		ownerDisplay = book.DisplayName(ownerUsername)
	}

	rows, err := db.Query("SELECT member_id FROM chatroom_member WHERE room_id = ?", roomID)
	if err != nil {
		return MembersResult{}, err
	}
	defer rows.Close()
	var memberIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return MembersResult{}, err
		}
		memberIDs = append(memberIDs, id)
	}
	if err := rows.Err(); err != nil {
		return MembersResult{}, err
	}

	members := []GroupMember{}
	if len(memberIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(memberIDs)), ",")
		args := make([]interface{}, len(memberIDs))
		for i, id := range memberIDs {
			args[i] = id
		}
		q := fmt.Sprintf("SELECT id, username, nick_name, remark FROM contact WHERE id IN (%s)", placeholders)
		mrows, err := db.Query(q, args...)
		if err != nil {
			return MembersResult{}, err
		}
		defer mrows.Close()
		for mrows.Next() {
			var id int64
			var username, nick, remark sql.NullString
			if err := mrows.Scan(&id, &username, &nick, &remark); err != nil {
				return MembersResult{}, err
			}
			u := username.String
			n := nick.String
			r := remark.String
			display := r
			if display == "" {
				display = n
			}
			if display == "" {
				display = u
			}
			members = append(members, GroupMember{
				Username:    u,
				NickName:    n,
				Remark:      r,
				DisplayName: display,
			})
		}
		if err := mrows.Err(); err != nil {
			return MembersResult{}, err
		}
	}

	sort.Slice(members, func(i, j int) bool {
		mi, mj := members[i], members[j]
		rankI, rankJ := 1, 1
		if mi.Username == ownerUsername {
			rankI = 0
		}
		if mj.Username == ownerUsername {
			rankJ = 0
		}
		if rankI != rankJ {
			return rankI < rankJ
		}
		return mi.DisplayName < mj.DisplayName
	})

	return MembersResult{
		Username: chatroomUsername,
		Owner:    ownerDisplay,
		Members:  members,
	}, nil
}
