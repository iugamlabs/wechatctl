//go:build !windows

package backend

// SpawnFrameWatcher Linux 无需处理 Windows 经典灰框。
func SpawnFrameWatcher(rootPID uint32) error {
	return nil
}

// WatchWeixinFrames Linux 上为空操作。
func WatchWeixinFrames(rootPID uint32) error {
	return nil
}
