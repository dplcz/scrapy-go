package agent

import (
	"context"
	"time"
)

// ============================================================================
// 核心接口定义
// ============================================================================

// AgentServer 定义 Agent Daemon 服务端需要实现的核心接口。
//
// Agent Daemon 是运行在每台爬虫机器上的独立进程，负责：
//   - 接收并存储爬虫部署包（编译好的 Go 二进制）
//   - 启动/停止爬虫子进程
//   - 监控子进程状态和资源使用
//   - 向管理平台上报心跳和状态
//
// 所有方法均应是并发安全的。
type AgentServer interface {
	// Deploy 部署一个爬虫包到 Agent。
	// 接收编译好的 Go 二进制文件，存储到本地部署目录。
	// 返回部署后的项目信息。
	Deploy(ctx context.Context, req *DeployRequest) (*DeployResponse, error)

	// Schedule 调度启动一个爬虫 Job。
	// 从已部署的项目中查找指定爬虫，fork/exec 启动子进程。
	// 返回 Job ID 供后续查询和控制。
	Schedule(ctx context.Context, req *ScheduleRequest) (*ScheduleResponse, error)

	// Cancel 取消（停止）一个正在运行的 Job。
	// 向子进程发送优雅停止信号，超时后强制终止。
	Cancel(ctx context.Context, req *CancelRequest) (*CancelResponse, error)

	// ListJobs 列出所有 Job 的状态（pending/running/finished）。
	ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error)

	// ListSpiders 列出指定项目中所有可用的爬虫名称。
	ListSpiders(ctx context.Context, req *ListSpidersRequest) (*ListSpidersResponse, error)

	// ListProjects 列出所有已部署的项目。
	ListProjects(ctx context.Context) (*ListProjectsResponse, error)

	// DeleteProject 删除一个已部署的项目及其所有版本。
	// 如果项目有正在运行的 Job，返回错误。
	DeleteProject(ctx context.Context, req *DeleteProjectRequest) error

	// GetJobLog 获取指定 Job 的日志输出。
	GetJobLog(ctx context.Context, req *GetJobLogRequest) (*GetJobLogResponse, error)

	// Status 获取 Agent 节点的状态信息。
	Status(ctx context.Context) (*NodeStatus, error)
}

// AgentClient 定义管理平台调用 Agent Daemon 的客户端接口。
//
// 管理平台通过此接口与远程 Agent 通信，实现：
//   - 远程部署爬虫包
//   - 远程启动/停止爬虫
//   - 查询 Agent 状态和 Job 列表
//
// 实现应处理网络超时、重试和错误转换。
type AgentClient interface {
	AgentServer

	// Ping 检查 Agent 是否可达。
	Ping(ctx context.Context) error

	// Close 关闭客户端连接，释放资源。
	Close() error
}

// HeartbeatReporter 定义 Agent 向管理平台上报心跳的接口。
//
// Agent Daemon 定期调用此接口向管理平台上报：
//   - 节点存活状态
//   - 资源使用情况（CPU、内存、磁盘）
//   - 当前运行的 Job 列表
//
// 管理平台根据心跳信息维护 Agent 节点列表，
// 超时未收到心跳的节点将被标记为离线。
type HeartbeatReporter interface {
	// ReportHeartbeat 上报一次心跳。
	ReportHeartbeat(ctx context.Context, heartbeat *Heartbeat) error
}

// ============================================================================
// 请求/响应数据传输对象
// ============================================================================

// DeployRequest 是部署爬虫包的请求。
type DeployRequest struct {
	// Project 是项目名称（必填）。
	// 同一项目可以有多个版本，新版本会覆盖旧版本。
	Project string `json:"project"`

	// Version 是部署版本标识（可选）。
	// 为空时使用时间戳自动生成。
	Version string `json:"version,omitempty"`

	// BinaryData 是编译好的 Go 二进制文件内容。
	// 通过 HTTP multipart/form-data 上传。
	BinaryData []byte `json:"-"`

	// Spiders 是该二进制中包含的爬虫名称列表（可选）。
	// 为空时 Agent 会尝试通过 --list-spiders 参数自动发现。
	Spiders []string `json:"spiders,omitempty"`
}

// DeployResponse 是部署爬虫包的响应。
type DeployResponse struct {
	// Project 是项目名称。
	Project string `json:"project"`

	// Version 是实际使用的版本标识。
	Version string `json:"version"`

	// Spiders 是发现的爬虫名称列表。
	Spiders []string `json:"spiders"`

	// DeployedAt 是部署时间。
	DeployedAt time.Time `json:"deployed_at"`
}

// ScheduleRequest 是调度启动爬虫的请求。
type ScheduleRequest struct {
	// Project 是项目名称（必填）。
	Project string `json:"project"`

	// Spider 是爬虫名称（必填）。
	Spider string `json:"spider"`

	// JobID 是自定义 Job ID（可选）。
	// 为空时自动生成 UUID。
	JobID string `json:"job_id,omitempty"`

	// Priority 是 Job 优先级（可选，默认 0）。
	// 数值越大优先级越高。当并发 Job 数达到上限时，
	// 高优先级 Job 会优先执行。
	Priority int `json:"priority,omitempty"`

	// Settings 是运行时配置覆盖（可选）。
	// 以命令行参数形式传递给爬虫子进程。
	Settings map[string]string `json:"settings,omitempty"`

	// Args 是传递给爬虫的自定义参数（可选）。
	// 以环境变量形式注入到子进程。
	Args map[string]string `json:"args,omitempty"`
}

// ScheduleResponse 是调度启动爬虫的响应。
type ScheduleResponse struct {
	// JobID 是分配的 Job ID。
	JobID string `json:"job_id"`

	// Status 是 Job 的初始状态（通常为 "pending" 或 "running"）。
	Status JobStatus `json:"status"`
}

// CancelRequest 是取消 Job 的请求。
type CancelRequest struct {
	// Project 是项目名称（必填）。
	Project string `json:"project"`

	// JobID 是要取消的 Job ID（必填）。
	JobID string `json:"job_id"`

	// Signal 是发送给子进程的信号（可选，默认 SIGTERM）。
	// 支持 "SIGTERM"、"SIGINT"、"SIGKILL"。
	Signal string `json:"signal,omitempty"`
}

// CancelResponse 是取消 Job 的响应。
type CancelResponse struct {
	// PrevState 是取消前的 Job 状态。
	PrevState JobStatus `json:"prev_state"`
}

// ListJobsRequest 是列出 Job 的请求。
type ListJobsRequest struct {
	// Project 是项目名称（可选）。
	// 为空时列出所有项目的 Job。
	Project string `json:"project,omitempty"`
}

// ListJobsResponse 是列出 Job 的响应。
type ListJobsResponse struct {
	// Pending 是等待执行的 Job 列表。
	Pending []JobInfo `json:"pending"`

	// Running 是正在执行的 Job 列表。
	Running []JobInfo `json:"running"`

	// Finished 是已完成的 Job 列表（最近 N 条）。
	Finished []JobInfo `json:"finished"`
}

// ListSpidersRequest 是列出爬虫的请求。
type ListSpidersRequest struct {
	// Project 是项目名称（必填）。
	Project string `json:"project"`
}

// ListSpidersResponse 是列出爬虫的响应。
type ListSpidersResponse struct {
	// Spiders 是爬虫名称列表。
	Spiders []string `json:"spiders"`
}

// ListProjectsResponse 是列出项目的响应。
type ListProjectsResponse struct {
	// Projects 是项目信息列表。
	Projects []ProjectInfo `json:"projects"`
}

// DeleteProjectRequest 是删除项目的请求。
type DeleteProjectRequest struct {
	// Project 是项目名称（必填）。
	Project string `json:"project"`
}

// GetJobLogRequest 是获取 Job 日志的请求。
type GetJobLogRequest struct {
	// Project 是项目名称（必填）。
	Project string `json:"project"`

	// JobID 是 Job ID（必填）。
	JobID string `json:"job_id"`

	// Offset 是日志偏移量（字节，可选，默认 0）。
	Offset int64 `json:"offset,omitempty"`

	// Limit 是返回的最大字节数（可选，默认 64KB）。
	Limit int64 `json:"limit,omitempty"`
}

// GetJobLogResponse 是获取 Job 日志的响应。
type GetJobLogResponse struct {
	// Content 是日志内容。
	Content string `json:"content"`

	// Offset 是当前偏移量。
	Offset int64 `json:"offset"`

	// TotalSize 是日志文件总大小。
	TotalSize int64 `json:"total_size"`
}

// ============================================================================
// 数据模型
// ============================================================================

// JobStatus 表示 Job 的状态。
type JobStatus string

const (
	// JobStatusPending 表示 Job 等待执行。
	JobStatusPending JobStatus = "pending"

	// JobStatusRunning 表示 Job 正在执行。
	JobStatusRunning JobStatus = "running"

	// JobStatusFinished 表示 Job 已完成（正常退出）。
	JobStatusFinished JobStatus = "finished"

	// JobStatusCancelled 表示 Job 被取消。
	JobStatusCancelled JobStatus = "cancelled"

	// JobStatusFailed 表示 Job 执行失败（非零退出码）。
	JobStatusFailed JobStatus = "failed"
)

// JobInfo 表示一个 Job 的详细信息。
type JobInfo struct {
	// ID 是 Job 的唯一标识。
	ID string `json:"id"`

	// Project 是所属项目名称。
	Project string `json:"project"`

	// Spider 是爬虫名称。
	Spider string `json:"spider"`

	// Status 是当前状态。
	Status JobStatus `json:"status"`

	// PID 是子进程 PID（仅 running 状态有值）。
	PID int `json:"pid,omitempty"`

	// StartTime 是启动时间。
	StartTime *time.Time `json:"start_time,omitempty"`

	// EndTime 是结束时间（仅 finished/cancelled/failed 状态有值）。
	EndTime *time.Time `json:"end_time,omitempty"`

	// ExitCode 是进程退出码（仅 finished/failed 状态有值）。
	ExitCode *int `json:"exit_code,omitempty"`

	// Priority 是 Job 优先级。
	Priority int `json:"priority,omitempty"`

	// Settings 是运行时配置覆盖。
	Settings map[string]string `json:"settings,omitempty"`

	// Args 是自定义参数。
	Args map[string]string `json:"args,omitempty"`

	// Stats 是爬虫运行统计（仅 running/finished 状态有值）。
	Stats map[string]any `json:"stats,omitempty"`
}

// ProjectInfo 表示一个已部署项目的信息。
type ProjectInfo struct {
	// Name 是项目名称。
	Name string `json:"name"`

	// Version 是当前活跃版本。
	Version string `json:"version"`

	// Spiders 是该项目包含的爬虫名称列表。
	Spiders []string `json:"spiders"`

	// DeployedAt 是最后部署时间。
	DeployedAt time.Time `json:"deployed_at"`
}

// NodeStatus 表示 Agent 节点的状态信息。
type NodeStatus struct {
	// NodeID 是节点唯一标识。
	NodeID string `json:"node_id"`

	// Hostname 是主机名。
	Hostname string `json:"hostname"`

	// Platform 是操作系统平台（如 "linux/amd64"）。
	Platform string `json:"platform"`

	// StartedAt 是 Agent Daemon 启动时间。
	StartedAt time.Time `json:"started_at"`

	// Uptime 是运行时长。
	Uptime time.Duration `json:"uptime"`

	// MaxConcurrentJobs 是最大并发 Job 数。
	MaxConcurrentJobs int `json:"max_concurrent_jobs"`

	// RunningJobs 是当前运行中的 Job 数。
	RunningJobs int `json:"running_jobs"`

	// PendingJobs 是等待执行的 Job 数。
	PendingJobs int `json:"pending_jobs"`

	// FinishedJobs 是已完成的 Job 总数。
	FinishedJobs int `json:"finished_jobs"`

	// Resources 是节点资源使用情况。
	Resources *ResourceUsage `json:"resources,omitempty"`
}

// ResourceUsage 表示节点资源使用情况。
type ResourceUsage struct {
	// CPUPercent 是 CPU 使用率（0-100）。
	CPUPercent float64 `json:"cpu_percent"`

	// MemoryUsedMB 是已使用内存（MB）。
	MemoryUsedMB int64 `json:"memory_used_mb"`

	// MemoryTotalMB 是总内存（MB）。
	MemoryTotalMB int64 `json:"memory_total_mb"`

	// DiskUsedMB 是已使用磁盘空间（MB）。
	DiskUsedMB int64 `json:"disk_used_mb"`

	// DiskTotalMB 是总磁盘空间（MB）。
	DiskTotalMB int64 `json:"disk_total_mb"`
}

// Heartbeat 表示 Agent 上报的心跳信息。
type Heartbeat struct {
	// NodeID 是节点唯一标识。
	NodeID string `json:"node_id"`

	// Timestamp 是心跳时间戳。
	Timestamp time.Time `json:"timestamp"`

	// Status 是节点状态摘要。
	Status *NodeStatus `json:"status"`

	// RunningJobs 是当前运行中的 Job 摘要列表。
	RunningJobs []JobSummary `json:"running_jobs,omitempty"`
}

// JobSummary 是 Job 的简要信息，用于心跳上报。
type JobSummary struct {
	// ID 是 Job ID。
	ID string `json:"id"`

	// Project 是项目名称。
	Project string `json:"project"`

	// Spider 是爬虫名称。
	Spider string `json:"spider"`

	// StartTime 是启动时间。
	StartTime time.Time `json:"start_time"`

	// ItemsScraped 是已抓取的 Item 数量。
	ItemsScraped int64 `json:"items_scraped"`

	// RequestCount 是已发送的请求数量。
	RequestCount int64 `json:"request_count"`
}

// ============================================================================
// 配置
// ============================================================================

// Config 是 Agent Daemon 的配置。
type Config struct {
	// NodeID 是节点唯一标识（可选）。
	// 为空时自动生成（基于 hostname + 随机后缀）。
	NodeID string `json:"node_id,omitempty" toml:"node_id"`

	// ListenAddr 是 HTTP API 监听地址（默认 ":6800"）。
	// 对齐 scrapyd 默认端口。
	ListenAddr string `json:"listen_addr,omitempty" toml:"listen_addr"`

	// MaxConcurrentJobs 是最大并发 Job 数（默认为 CPU 核心数）。
	MaxConcurrentJobs int `json:"max_concurrent_jobs,omitempty" toml:"max_concurrent_jobs"`

	// ProjectDir 是爬虫部署包存储目录（默认 "./projects"）。
	ProjectDir string `json:"project_dir,omitempty" toml:"project_dir"`

	// LogDir 是 Job 日志存储目录（默认 "./logs"）。
	LogDir string `json:"log_dir,omitempty" toml:"log_dir"`

	// PIDFile 是 PID 文件路径（可选）。
	// 设置后 daemon 启动时写入 PID，用于进程管理。
	PIDFile string `json:"pid_file,omitempty" toml:"pid_file"`

	// FinishedJobsRetention 是已完成 Job 信息的保留数量（默认 100）。
	FinishedJobsRetention int `json:"finished_jobs_retention,omitempty" toml:"finished_jobs_retention"`

	// GracefulShutdownTimeout 是优雅关闭超时时间（默认 30s）。
	// 超时后强制终止所有子进程。
	GracefulShutdownTimeout time.Duration `json:"graceful_shutdown_timeout,omitempty" toml:"graceful_shutdown_timeout"`

	// Heartbeat 是心跳上报配置（可选）。
	// 为 nil 时不启用心跳上报。
	Heartbeat *HeartbeatConfig `json:"heartbeat,omitempty" toml:"heartbeat"`
}

// HeartbeatConfig 是心跳上报配置。
type HeartbeatConfig struct {
	// Enabled 是否启用心跳上报。
	Enabled bool `json:"enabled" toml:"enabled"`

	// Endpoint 是管理平台的心跳接收端点 URL。
	Endpoint string `json:"endpoint" toml:"endpoint"`

	// Interval 是心跳上报间隔（默认 30s）。
	Interval time.Duration `json:"interval,omitempty" toml:"interval"`

	// Timeout 是单次心跳请求超时（默认 5s）。
	Timeout time.Duration `json:"timeout,omitempty" toml:"timeout"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:              ":6800",
		MaxConcurrentJobs:       0, // 0 表示使用 CPU 核心数
		ProjectDir:              "./projects",
		LogDir:                  "./logs",
		FinishedJobsRetention:   100,
		GracefulShutdownTimeout: 30 * time.Second,
	}
}
