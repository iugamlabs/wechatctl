package errkind

// Error 是数据命令的哨兵错误（exit code 在 Code 字段）。
type Error struct {
	Code int
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

var (
	ErrInstanceNotFound = &Error{Code: 1, Msg: "instance not found"}
	ErrChatNotFound     = &Error{Code: 1, Msg: "chat not found"}
	ErrNotGroup         = &Error{Code: 1, Msg: "not a group chat"}
	ErrNoDBStorage      = &Error{Code: 1, Msg: "no db_storage"}
	ErrInvalidArgs      = &Error{Code: 2, Msg: "invalid arguments"}
	ErrNoKeys           = &Error{Code: 3, Msg: "keys not found"}
	ErrDecrypt          = &Error{Code: 3, Msg: "decrypt failed"}
	ErrNotRunning       = &Error{Code: 4, Msg: "instance not running"}
	ErrPermission       = &Error{Code: 5, Msg: "permission denied"}
	ErrSchema           = &Error{Code: 6, Msg: "schema mismatch"}
)
