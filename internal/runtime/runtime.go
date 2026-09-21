package runtime

import (
	"time"

	"github.com/star-plan/wechatctl/internal/backend"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

// Status 描述实例进程是否在运行。
type Status = backend.Status

// ExitError 保留子进程退出码，供 CLI 透传。
type ExitError = backend.ExitError

// Manager 是进程启停的门面，实际工作交给平台 Backend。
type Manager struct {
	Layout paths.Layout
	Config config.Config
}

func (m Manager) backend() backend.Backend {
	return backend.New(m.Layout, m.Config)
}

// Start 在前台启动微信并等待退出。
func (m Manager) Start(inst config.Instance, extraArgs []string) error {
	return m.StartWith(inst, backend.StartOptions{ExtraArgs: extraArgs})
}

// StartWith 按选项启动微信。
func (m Manager) StartWith(inst config.Instance, opts backend.StartOptions) error {
	return m.backend().Start(inst, opts)
}

// Probe 返回单个实例的运行状态。
func (m Manager) Probe(name string) Status {
	return m.backend().Status(name)
}

// StatusAll 查询多个实例的运行状态。
func (m Manager) StatusAll(names []string) []Status {
	out := make([]Status, 0, len(names))
	for _, n := range names {
		out = append(out, m.Probe(n))
	}
	return out
}

// Stop 停止指定实例。
func (m Manager) Stop(name string, timeout time.Duration) error {
	return m.backend().Stop(name, timeout)
}

// InstancePIDs 返回本实例微信主进程（pid 文件，沿用 Status）以及
// /proc 中 fail-closed 匹配 WXCTL_INSTANCE 的 wechat* 子进程，按 RSS 降序去重。
// 不调用 backend.belongsToInstance（environ 读失败会 fail-open）。
func (m Manager) InstancePIDs(name string) []int {
	seen := make(map[int]struct{})
	var pids []int
	add := func(pid int) {
		if pid <= 0 {
			return
		}
		if _, ok := seen[pid]; ok {
			return
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	if st := m.Probe(name); st.Running {
		add(st.PID)
	}
	for _, pid := range keys.ListInstanceWeChatPIDs(name) {
		add(pid)
	}
	keys.SortPIDsByRSS(pids)
	return pids
}
