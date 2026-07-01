package storage

// SnapshotStatus 池子快照生命周期状态。
type SnapshotStatus string

const (
	SnapshotInitializing SnapshotStatus = "INITIALIZING" // 正在 Snapshot
	SnapshotReady        SnapshotStatus = "READY"        // Snapshot 完成，可加载
	SnapshotFailed       SnapshotStatus = "FAILED"       // Snapshot 失败
	SnapshotDisabled     SnapshotStatus = "DISABLED"     // 人工禁用
)

// ParseSnapshotStatus 解析状态字符串，未知值返回空。
func ParseSnapshotStatus(s string) SnapshotStatus {
	switch SnapshotStatus(s) {
	case SnapshotInitializing, SnapshotReady, SnapshotFailed, SnapshotDisabled:
		return SnapshotStatus(s)
	default:
		return ""
	}
}
