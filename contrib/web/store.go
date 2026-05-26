package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RunRecord 表示一次爬虫运行的历史记录。
type RunRecord struct {
	// ID 是运行实例的唯一标识。
	ID string `json:"id"`

	// SpiderName 是 Spider 注册名称。
	SpiderName string `json:"spider_name"`

	// StartTime 是启动时间。
	StartTime time.Time `json:"start_time"`

	// EndTime 是结束时间（运行中为零值）。
	EndTime time.Time `json:"end_time,omitempty"`

	// Duration 是运行时长（秒）。
	Duration float64 `json:"duration,omitempty"`

	// Status 是运行状态：running / finished / stopped / error。
	Status string `json:"status"`

	// Args 是启动时传入的参数。
	Args map[string]any `json:"args,omitempty"`

	// Stats 是运行结束时的统计快照。
	Stats map[string]any `json:"stats,omitempty"`

	// Error 是运行错误信息（如果有）。
	Error string `json:"error,omitempty"`
}

// Store 是爬取历史持久化存储。
//
// 提供运行记录的增删查改和统计快照存储。
// 支持可选的 JSON 文件持久化（通过 WithStorePath 配置）。
//
// 线程安全：所有公共方法均可被多个 goroutine 安全调用。
type Store struct {
	mu      sync.RWMutex
	records []*RunRecord

	// maxRecords 是最大保留记录数（默认 1000）。
	maxRecords int

	// persistPath 是 JSON 持久化文件路径（为空则不持久化）。
	persistPath string
}

// StoreOption 是 Store 的可选配置函数。
type StoreOption func(*Store)

// WithMaxRecords 设置最大保留记录数。
func WithMaxRecords(n int) StoreOption {
	return func(s *Store) {
		if n > 0 {
			s.maxRecords = n
		}
	}
}

// WithStorePath 设置 JSON 持久化文件路径。
// 设置后，Store 会在启动时加载历史记录，并在每次变更时写入文件。
func WithStorePath(path string) StoreOption {
	return func(s *Store) {
		s.persistPath = path
	}
}

// NewStore 创建一个新的历史记录存储。
func NewStore(opts ...StoreOption) *Store {
	s := &Store{
		records:    make([]*RunRecord, 0, 64),
		maxRecords: 1000,
	}
	for _, opt := range opts {
		opt(s)
	}

	// 如果配置了持久化路径，尝试加载历史记录
	if s.persistPath != "" {
		_ = s.loadFromFile()
	}

	return s
}

// RecordStart 记录一次爬虫启动。
func (s *Store) RecordStart(id, spiderName string, args map[string]any) {
	record := &RunRecord{
		ID:         id,
		SpiderName: spiderName,
		StartTime:  time.Now(),
		Status:     "running",
		Args:       args,
	}

	s.mu.Lock()
	s.records = append(s.records, record)
	s.trimLocked()
	s.mu.Unlock()

	s.persist()
}

// RecordFinish 记录一次爬虫运行结束。
func (s *Store) RecordFinish(id string, stats map[string]any, errMsg string) {
	s.mu.Lock()
	for _, r := range s.records {
		if r.ID == id {
			r.EndTime = time.Now()
			r.Duration = r.EndTime.Sub(r.StartTime).Seconds()
			r.Stats = stats
			if errMsg != "" {
				r.Status = "error"
				r.Error = errMsg
			} else {
				r.Status = "finished"
			}
			break
		}
	}
	s.mu.Unlock()

	s.persist()
}

// RecordStop 记录一次爬虫被手动停止。
func (s *Store) RecordStop(id string, stats map[string]any) {
	s.mu.Lock()
	for _, r := range s.records {
		if r.ID == id {
			r.EndTime = time.Now()
			r.Duration = r.EndTime.Sub(r.StartTime).Seconds()
			r.Status = "stopped"
			r.Stats = stats
			break
		}
	}
	s.mu.Unlock()

	s.persist()
}

// GetRecord 获取指定 ID 的运行记录。
func (s *Store) GetRecord(id string) (*RunRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.records {
		if r.ID == id {
			return r, true
		}
	}
	return nil, false
}

// GetRecordsBySpider 获取指定 Spider 的所有运行记录（按时间倒序）。
func (s *Store) GetRecordsBySpider(spiderName string, limit int) []*RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*RunRecord
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].SpiderName == spiderName {
			results = append(results, s.records[i])
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

// GetRecentRecords 获取最近的运行记录（按时间倒序）。
func (s *Store) GetRecentRecords(limit int) []*RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.records) {
		limit = len(s.records)
	}

	results := make([]*RunRecord, limit)
	for i := 0; i < limit; i++ {
		results[i] = s.records[len(s.records)-1-i]
	}
	return results
}

// GetAllRecords 获取所有运行记录（按时间倒序）。
func (s *Store) GetAllRecords() []*RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*RunRecord, len(s.records))
	for i, r := range s.records {
		results[len(s.records)-1-i] = r
	}
	return results
}

// Len 返回记录总数。
func (s *Store) Len() int {
	s.mu.RLock()
	n := len(s.records)
	s.mu.RUnlock()
	return n
}

// GetStats 获取汇总统计信息。
func (s *Store) GetStats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.records)
	var running, finished, stopped, errored int
	spiders := make(map[string]int)

	for _, r := range s.records {
		spiders[r.SpiderName]++
		switch r.Status {
		case "running":
			running++
		case "finished":
			finished++
		case "stopped":
			stopped++
		case "error":
			errored++
		}
	}

	return map[string]any{
		"total_runs":     total,
		"running":        running,
		"finished":       finished,
		"stopped":        stopped,
		"errors":         errored,
		"unique_spiders": len(spiders),
	}
}

// ============================================================================
// 内部方法
// ============================================================================

// trimLocked 在持有锁的情况下裁剪超出上限的旧记录。
func (s *Store) trimLocked() {
	if len(s.records) > s.maxRecords {
		excess := len(s.records) - s.maxRecords
		s.records = s.records[excess:]
	}
}

// persist 将记录持久化到文件（如果配置了路径）。
func (s *Store) persist() {
	if s.persistPath == "" {
		return
	}

	s.mu.RLock()
	data, err := json.MarshalIndent(s.records, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return
	}

	// 确保目录存在
	dir := filepath.Dir(s.persistPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	// 原子写入：先写临时文件再重命名
	tmpPath := s.persistPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.persistPath)
}

// loadFromFile 从文件加载历史记录。
func (s *Store) loadFromFile() error {
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var records []*RunRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}

	// 按时间排序
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartTime.Before(records[j].StartTime)
	})

	s.records = records
	return nil
}
