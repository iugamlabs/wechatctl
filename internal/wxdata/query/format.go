package query

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

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
