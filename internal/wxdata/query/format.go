package query

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

var (
	zstdDecoder     *zstd.Decoder
	zstdDecoderOnce sync.Once
)

func zstdDec() *zstd.Decoder {
	zstdDecoderOnce.Do(func() {
		zstdDecoder, _ = zstd.NewReader(nil)
	})
	return zstdDecoder
}

// SplitMsgType 拆分 local_type 为 base 与 sub（对齐 messages.py）。
func SplitMsgType(t int64) (base uint32, sub uint32) {
	u := uint64(t)
	return uint32(u), uint32(u >> 32)
}

// FormatMsgType 返回中文类型标签（JSON msg_type / type 字段）。
func FormatMsgType(t int64) string {
	base, _ := SplitMsgType(t)
	switch base {
	case 1:
		return "文本"
	case 3:
		return "图片"
	case 34:
		return "语音"
	case 42:
		return "名片"
	case 43:
		return "视频"
	case 47:
		return "表情"
	case 48:
		return "位置"
	case 49:
		return "链接/文件"
	case 50:
		return "通话"
	case 10000:
		return "系统"
	case 10002:
		return "撤回"
	default:
		return "type=" + strconv.FormatInt(t, 10)
	}
}

// DecompressContent 解压消息/summary 内容；ok==false 表示 zstd 解压失败（ct==4）。
func DecompressContent(content []byte, ct int) (string, bool) {
	if content == nil {
		return "", true
	}
	if ct == 4 {
		dec := zstdDec()
		if dec == nil {
			return "", false
		}
		out, err := dec.DecodeAll(content, nil)
		if err != nil {
			return "", false
		}
		return string(out), true
	}
	return string(content), true
}

// SessionSummaryPlaceholder 是 sessions/unread/new-messages 解压失败占位。
const SessionSummaryPlaceholder = "(压缩内容)"

// FormatSessionSummary 解压并处理群摘要 `:\n` 切割（ct 固定为 4，与 sessions.py 一致）。
func FormatSessionSummary(summary interface{}) string {
	switch v := summary.(type) {
	case nil:
		return ""
	case string:
		return SplitGroupSummary(v)
	case []byte:
		text, ok := DecompressContent(v, 4)
		if !ok {
			return SessionSummaryPlaceholder
		}
		return SplitGroupSummary(text)
	default:
		return SplitGroupSummary(fmt.Sprint(v))
	}
}

// SplitGroupSummary 若含 `:\n` 取后半（群 last_message）。
func SplitGroupSummary(summary string) string {
	if idx := strings.Index(summary, ":\n"); idx >= 0 {
		return summary[idx+2:]
	}
	return summary
}

// HistoryContentPlaceholder 是 history/chat-export 解压失败占位。
const HistoryContentPlaceholder = "(无法解压)"

var (
	xmlUnsafeRE    = regexp.MustCompile(`(?i)<!DOCTYPE|<!ENTITY`)
	xmlParseMaxLen = 20000
)

// ParseMessageContent 从群消息 content 拆出 sender wxid 与正文。
func ParseMessageContent(content string, isGroup bool) (sender string, text string) {
	if content == "" {
		return "", ""
	}
	text = content
	if isGroup {
		if idx := strings.Index(content, ":\n"); idx >= 0 {
			sender = content[:idx]
			text = content[idx+2:]
		}
	}
	return sender, text
}

func collapseText(text string) string {
	if text == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

func parseXMLRoot(content string) bool {
	if content == "" || len(content) > xmlParseMaxLen || xmlUnsafeRE.MatchString(content) {
		return false
	}
	var v struct {
		XMLName xml.Name
	}
	return xml.Unmarshal([]byte(content), &v) == nil
}

type appmsgXML struct {
	AppMsg struct {
		Title    string `xml:"title"`
		Type     string `xml:"type"`
		ReferMsg struct {
			DisplayName string `xml:"displayname"`
			Content     string `xml:"content"`
		} `xml:"refermsg"`
	} `xml:"appmsg"`
}

func parseAppMsg(content string) (*appmsgXML, bool) {
	if !parseXMLRoot(content) || !strings.Contains(content, "<appmsg") {
		return nil, false
	}
	var root appmsgXML
	if err := xml.Unmarshal([]byte(content), &root); err != nil {
		return nil, false
	}
	return &root, true
}

func parseIntDefault(s string, fallback int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

// FormatAppMessageText 解析 appmsg XML（resolve_media 恒为 false）。
func FormatAppMessageText(content string, localType int64) string {
	if content == "" || !strings.Contains(content, "<appmsg") {
		return ""
	}
	_, sub := SplitMsgType(localType)
	root, ok := parseAppMsg(content)
	if !ok {
		return ""
	}
	app := root.AppMsg
	title := collapseText(app.Title)
	appType := parseIntDefault(app.Type, int(sub))

	switch appType {
	case 57:
		refContent := collapseText(app.ReferMsg.Content)
		refName := strings.TrimSpace(app.ReferMsg.DisplayName)
		if len(refContent) > 160 {
			refContent = refContent[:160] + "..."
		}
		quote := title
		if quote == "" {
			quote = "[引用消息]"
		}
		if refContent != "" {
			prefix := "回复: "
			if refName != "" {
				prefix = "回复 " + refName + ": "
			}
			quote += "\n  ↳ " + prefix + refContent
		}
		return quote
	case 6:
		if title != "" {
			return "[文件] " + title
		}
		return "[文件]"
	case 5:
		if title != "" {
			return "[链接] " + title
		}
		return "[链接]"
	case 33, 36, 44:
		if title != "" {
			return "[小程序] " + title
		}
		return "[小程序]"
	}
	if title != "" {
		return "[链接/文件] " + title
	}
	return "[链接/文件]"
}

// FormatVoipMessageText 简化 voip XML。
func FormatVoipMessageText(content string) string {
	if content == "" || !strings.Contains(content, "<voip") {
		return ""
	}
	if !parseXMLRoot(content) {
		return "[通话]"
	}
	var root struct {
		Msg string `xml:"msg"`
	}
	if err := xml.Unmarshal([]byte(content), &root); err != nil {
		return "[通话]"
	}
	raw := collapseText(root.Msg)
	if raw == "" {
		return "[通话]"
	}
	status := map[string]string{
		"Canceled":            "已取消",
		"Line busy":           "对方忙线",
		"Call not answered":   "未接听",
		"Call wasn't answered": "未接听",
	}
	if strings.HasPrefix(raw, "Duration:") {
		duration := strings.TrimSpace(strings.TrimPrefix(raw, "Duration:"))
		if duration == "" {
			return "[通话]"
		}
		return "[通话] 通话时长 " + duration
	}
	if mapped, ok := status[raw]; ok {
		return "[通话] " + mapped
	}
	return "[通话] " + raw
}

// FormatMessageText 生成消息正文（不含 sender 标签）。
func FormatMessageText(localID, localType int64, content string, isGroup bool) (senderFromContent string, text string) {
	senderFromContent, text = ParseMessageContent(content, isGroup)
	base, _ := SplitMsgType(localType)

	switch base {
	case 1:
		return senderFromContent, text
	case 3:
		return senderFromContent, fmt.Sprintf("[图片] (local_id=%d)", localID)
	case 47:
		return senderFromContent, "[表情]"
	case 50:
		if v := FormatVoipMessageText(text); v != "" {
			return senderFromContent, v
		}
		return senderFromContent, "[通话]"
	case 49:
		if v := FormatAppMessageText(text, localType); v != "" {
			return senderFromContent, v
		}
		return senderFromContent, "[链接/文件]"
	default:
		label := FormatMsgType(localType)
		if text != "" {
			return senderFromContent, fmt.Sprintf("[%s] %s", label, text)
		}
		return senderFromContent, fmt.Sprintf("[%s]", label)
	}
}

// ResolveSenderLabel 解析发送者显示名（history/search JSON sender 字段）。
func ResolveSenderLabel(realSenderID int64, senderFromContent string, isGroup bool, chatUsername, chatDisplayName string, idToUsername map[int64]string, book *Book, dbDir string) string {
	senderUsername := idToUsername[realSenderID]
	if isGroup {
		if senderUsername != "" && senderUsername != chatUsername {
			return book.DisplayNameFor(senderUsername, dbDir)
		}
		if senderFromContent != "" {
			return book.DisplayNameFor(senderFromContent, dbDir)
		}
		return ""
	}
	if senderUsername == chatUsername {
		return chatDisplayName
	}
	if senderUsername != "" {
		return book.DisplayNameFor(senderUsername, dbDir)
	}
	return ""
}

// FormatMessageTime 格式化为 history/search 用的 time 字段。
func FormatMessageTime(ts int64) string {
	return time.Unix(ts, 0).Local().Format("2006-01-02 15:04")
}
