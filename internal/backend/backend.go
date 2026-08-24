package backend

import (
	"fmt"
	"time"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

const (
	// BackendLinuxHome 通过独立 HOME 目录隔离实例。
	BackendLinuxHome = "linux-home"
)

// Status 描述实例进程是否在运行。
type Status struct {
	Name    string
	Running bool
	PID     int
	Error   string
}

// ExitError 保留子进程退出码，供 CLI 透传。
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("wechat exited with status %d", e.Code)
}

// StartOptions 控制微信的启动方式。
type StartOptions struct {
	ExtraArgs []string
	Detach    bool
}

// CreateResult 保存需要写入实例注册表的平台字段。
type CreateResult struct {
	Backend string
}

// Backend 负责平台相关的实例隔离与进程控制。
type Backend interface {
	// Name 返回后端标识。
	Name() string
	// Create 创建实例隔离目录。
	Create(inst config.Instance) (CreateResult, error)
	// Start 启动该实例的微信进程。
	Start(inst config.Instance, opts StartOptions) error
	// Stop 停止该实例的微信进程。
	Stop(name string, timeout time.Duration) error
	// Remove 在 purge 时拆除隔离环境。
	Remove(inst config.Instance, purge bool) error
	// Status 查询实例是否在运行。
	Status(name string) Status
	// HomeDir 返回实例数据目录。
	HomeDir(inst config.Instance) string
	// DisplayUser 返回 list/show 中展示的隔离身份。
	DisplayUser(inst config.Instance) string
	// SharedDir 返回该实例可访问的共享目录。
	SharedDir(inst config.Instance) string
}

// New 构造当前操作系统对应的隔离后端。
func New(layout paths.Layout, cfg config.Config) Backend {
	return newPlatformBackend(layout, cfg)
}
