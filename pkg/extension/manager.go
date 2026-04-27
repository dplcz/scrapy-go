package extension

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
)

// Entry 表示一个带优先级的扩展条目。
type Entry struct {
	Extension Extension
	Name      string
	Priority  int
}

// Manager 管理扩展的生命周期。
// 对应 Scrapy 的 ExtensionManager。
//
// Manager 负责：
//   - 按优先级排序管理扩展
//   - 在 Spider 打开时初始化所有扩展
//   - 在 Spider 关闭时清理所有扩展
//   - 处理 ErrNotConfigured 错误（跳过未配置的扩展）
type Manager struct {
	extensions []Entry
	logger     *slog.Logger
}

// NewManager 创建一个新的扩展管理器。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger: logger,
	}
}

// AddExtension 添加一个扩展。
// 扩展按优先级排序，优先级数值小的先初始化。
func (m *Manager) AddExtension(ext Extension, name string, priority int) {
	m.extensions = append(m.extensions, Entry{
		Extension: ext,
		Name:      name,
		Priority:  priority,
	})
	// 按优先级排序（小的在前）
	sort.Slice(m.extensions, func(i, j int) bool {
		return m.extensions[i].Priority < m.extensions[j].Priority
	})
}

// Count 返回已注册的扩展数量。
func (m *Manager) Count() int {
	return len(m.extensions)
}

// Open 按优先级顺序打开所有扩展。
// 如果扩展返回 ErrNotConfigured，则跳过该扩展并记录调试日志。
// 其他错误将导致初始化失败。
func (m *Manager) Open(ctx context.Context) error {
	var active []Entry

	for _, entry := range m.extensions {
		if err := entry.Extension.Open(ctx); err != nil {
			if errors.Is(err, serrors.ErrNotConfigured) {
				m.logger.Debug("extension disabled (not configured)",
					"extension", entry.Name,
				)
				continue
			}
			m.logger.Error("failed to open extension",
				"extension", entry.Name,
				"error", err,
			)
			return err
		}
		active = append(active, entry)
	}

	// 仅保留成功打开的扩展
	m.extensions = active
	return nil
}

// Close 按逆序关闭所有扩展。
// 即使某个扩展关闭失败，也会继续关闭其他扩展。
func (m *Manager) Close(ctx context.Context) error {
	var firstErr error

	for i := len(m.extensions) - 1; i >= 0; i-- {
		entry := m.extensions[i]
		if err := entry.Extension.Close(ctx); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.logger.Error("failed to close extension",
				"extension", entry.Name,
				"error", err,
			)
		}
	}

	return firstErr
}

// Extensions 返回当前活跃的扩展列表快照。
func (m *Manager) Extensions() []Entry {
	result := make([]Entry, len(m.extensions))
	copy(result, m.extensions)
	return result
}
