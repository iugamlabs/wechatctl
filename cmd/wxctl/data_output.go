//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/star-plan/wechatctl/internal/wxdata/query"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type initDataResult struct {
	Instance string   `json:"instance"`
	DBDir    string   `json:"db_dir"`
	KeysFile string   `json:"keys_file"`
	Keys     int      `json:"keys"`
	Missing  []string `json:"missing"`
}

func writeInitDataText(w io.Writer, r initDataResult) {
	fmt.Fprintf(w, "initialized instance %q\n", r.Instance)
	fmt.Fprintf(w, "  db_dir:     %s\n", r.DBDir)
	fmt.Fprintf(w, "  keys_file:  %s\n", r.KeysFile)
	fmt.Fprintf(w, "  keys:       %d\n", r.Keys)
	fmt.Fprintf(w, "  missing:    %d\n", len(r.Missing))
}

func writeSessionsText(w io.Writer, items []query.SessionItem) {
	fmt.Fprintf(w, "最近 %d 个会话:\n\n", len(items))
	for i, r := range items {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeSessionLine(w, r)
	}
}

func writeSessionLine(w io.Writer, r query.SessionItem) {
	entry := fmt.Sprintf("[%s] %s", r.Time, r.Chat)
	if r.IsGroup {
		entry += " [群]"
	}
	if r.Unread > 0 {
		entry += fmt.Sprintf(" (%d条未读)", r.Unread)
	}
	entry += fmt.Sprintf("\n  %s: ", r.MsgType)
	if r.Sender != "" {
		entry += r.Sender + ": "
	}
	entry += r.LastMessage
	fmt.Fprintln(w, entry)
}

func writeUnreadText(w io.Writer, items []query.SessionItem) {
	if len(items) == 0 {
		fmt.Fprintln(w, "没有未读消息")
		return
	}
	fmt.Fprintf(w, "未读会话（%d 个）:\n\n", len(items))
	for i, r := range items {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeSessionLine(w, r)
	}
}

func writeContactsText(w io.Writer, items []query.ContactRow) {
	fmt.Fprintf(w, "找到 %d 个联系人:\n\n", len(items))
	for _, c := range items {
		display := c.Remark
		if display == "" {
			display = c.NickName
		}
		if display == "" {
			display = c.Username
		}
		line := fmt.Sprintf("%s  (%s)", display, c.Username)
		if c.Remark != "" {
			line += fmt.Sprintf("  备注: %s", c.Remark)
		}
		fmt.Fprintln(w, line)
	}
}

func writeContactDetailText(w io.Writer, info *query.ContactDetail) {
	lines := []string{fmt.Sprintf("联系人详情: %s", info.NickName)}
	if info.Remark != "" {
		lines = append(lines, "备注: "+info.Remark)
	}
	if info.Alias != "" {
		lines = append(lines, "微信号: "+info.Alias)
	}
	lines = append(lines, "wxid: "+info.Username)
	if info.Description != "" {
		lines = append(lines, "个性签名: "+info.Description)
	}
	if info.IsGroup {
		lines = append(lines, "类型: 群聊")
	} else if info.IsSubscription {
		lines = append(lines, "类型: 公众号")
	} else if info.VerifyFlag >= 8 {
		lines = append(lines, "类型: 企业认证")
	}
	if info.Avatar != "" {
		lines = append(lines, "头像: "+info.Avatar)
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}

func writeMembersText(w io.Writer, res query.MembersResult) {
	header := fmt.Sprintf("%s 的群成员（共 %d 人）", res.Group, res.MemberCount)
	if res.Owner != "" {
		header += "，群主: " + res.Owner
	}
	fmt.Fprintf(w, "%s:\n\n", header)
	for _, m := range res.Members {
		line := fmt.Sprintf("%s  (%s)", m.DisplayName, m.Username)
		if m.Remark != "" {
			line += "  备注: " + m.Remark
		}
		fmt.Fprintln(w, line)
	}
}

func writeNewMessagesText(w io.Writer, v interface{}) {
	switch r := v.(type) {
	case query.NewMessagesFirstResult:
		if len(r.Messages) == 0 {
			fmt.Fprintln(w, "当前无未读消息（已记录状态，下次调用将返回新消息）")
			return
		}
		fmt.Fprintf(w, "当前 %d 个未读会话:\n\n", r.UnreadCount)
		for _, m := range r.Messages {
			tag := ""
			if m.IsGroup {
				tag = " [群]"
			}
			fmt.Fprintf(w, "[%s] %s%s (%d条未读): %s\n", m.Time, m.Chat, tag, m.Unread, m.LastMessage)
		}
	case query.NewMessagesResult:
		if len(r.Messages) == 0 {
			fmt.Fprintln(w, "无新消息")
			return
		}
		fmt.Fprintf(w, "%d 条新消息:\n\n", r.NewCount)
		for _, m := range r.Messages {
			entry := fmt.Sprintf("[%s] %s", m.Time, m.Chat)
			if m.IsGroup {
				entry += " [群]"
			}
			entry += ": " + m.MsgType
			if m.Sender != "" {
				entry += " (" + m.Sender + ")"
			}
			entry += " - " + m.LastMessage
			fmt.Fprintln(w, entry)
		}
	}
}
