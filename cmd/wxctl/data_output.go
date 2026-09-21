//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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

func writeHistoryText(w io.Writer, res *query.HistoryResult) {
	if len(res.Messages) == 0 {
		fmt.Fprintf(w, "%s 无消息记录\n", res.Chat)
		return
	}
	header := fmt.Sprintf("%s 的消息记录（返回 %d 条，offset=%d, limit=%d）", res.Chat, res.Count, res.Offset, res.Limit)
	if res.IsGroup {
		header += " [群聊]"
	}
	if res.StartTime != nil || res.EndTime != nil {
		start := "最早"
		end := "最新"
		if res.StartTime != nil {
			start = *res.StartTime
		}
		if res.EndTime != nil {
			end = *res.EndTime
		}
		header += fmt.Sprintf("\n时间范围: %s ~ %s", start, end)
	}
	if res.Failures != nil && len(*res.Failures) > 0 {
		header += "\n查询失败: " + strings.Join(*res.Failures, "；")
	}
	fmt.Fprintln(w, header+":")
	fmt.Fprintln(w)
	for i, m := range res.Messages {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, m.ToTextLine())
	}
}

func writeSearchText(w io.Writer, res *query.SearchResult) {
	if len(res.Results) == 0 {
		fmt.Fprintf(w, "在 %s 中未找到包含 \"%s\" 的消息\n", res.Scope, res.Keyword)
		return
	}
	header := fmt.Sprintf("在 %s 中搜索 \"%s\" 找到 %d 条结果（offset=%d, limit=%d）", res.Scope, res.Keyword, res.Count, res.Offset, res.Limit)
	if res.StartTime != nil || res.EndTime != nil {
		start := "最早"
		end := "最新"
		if res.StartTime != nil {
			start = *res.StartTime
		}
		if res.EndTime != nil {
			end = *res.EndTime
		}
		header += fmt.Sprintf("\n时间范围: %s ~ %s", start, end)
	}
	if res.Failures != nil && len(*res.Failures) > 0 {
		header += "\n查询失败: " + strings.Join(*res.Failures, "；")
	}
	fmt.Fprintln(w, header+":")
	fmt.Fprintln(w)
	for i, h := range res.Results {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, h.ToSearchTextLine())
	}
}

func formatChatExport(format, displayName string, isGroup bool, startTime, endTime string, messages []query.Message) string {
	now := time.Now().Format("2006-01-02 15:04")
	chatType := "私聊"
	if isGroup {
		chatType = "群聊"
	}
	timeRange := fmt.Sprintf("%s ~ %s", orDefault(startTime, "最早"), orDefault(endTime, "最新"))
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.ToTextLine()
	}
	if format == "markdown" {
		header := fmt.Sprintf(
			"# 聊天记录: %s\n\n**时间范围:** %s\n\n**导出时间:** %s\n\n**消息数量:** %d\n\n**类型:** %s\n\n---\n",
			displayName, timeRange, now, len(messages), chatType,
		)
		items := make([]string, len(messages))
		for i, line := range lines {
			items[i] = "- " + line
		}
		return header + strings.Join(items, "\n")
	}
	header := fmt.Sprintf(
		"聊天记录: %s\n类型: %s\n时间范围: %s\n导出时间: %s\n消息数量: %d\n%s",
		displayName, chatType, timeRange, now, len(messages), strings.Repeat("=", 60),
	)
	return header + "\n" + strings.Join(lines, "\n")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
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
