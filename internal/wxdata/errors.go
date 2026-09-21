package wxdata

import "github.com/star-plan/wechatctl/internal/wxdata/errkind"

// Error 是数据命令的哨兵错误（exit code 在 Code 字段）。
type Error = errkind.Error

var (
	ErrInstanceNotFound = errkind.ErrInstanceNotFound
	ErrChatNotFound     = errkind.ErrChatNotFound
	ErrNotGroup         = errkind.ErrNotGroup
	ErrNoDBStorage      = errkind.ErrNoDBStorage
	ErrInvalidArgs      = errkind.ErrInvalidArgs
	ErrNoKeys           = errkind.ErrNoKeys
	ErrDecrypt          = errkind.ErrDecrypt
	ErrNotRunning       = errkind.ErrNotRunning
	ErrPermission       = errkind.ErrPermission
	ErrSchema           = errkind.ErrSchema
)
