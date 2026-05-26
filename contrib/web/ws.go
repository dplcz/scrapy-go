package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/signal"
)

// WSEvent 是 WebSocket 推送的事件结构。
type WSEvent struct {
	// Type 是事件类型（对应 Signal 名称）。
	Type string `json:"type"`

	// SpiderID 是关联的 Spider 运行 ID。
	SpiderID string `json:"spider_id,omitempty"`

	// SpiderName 是关联的 Spider 名称。
	SpiderName string `json:"spider_name,omitempty"`

	// Timestamp 是事件发生时间。
	Timestamp time.Time `json:"timestamp"`

	// Data 是事件携带的数据。
	Data map[string]any `json:"data,omitempty"`
}

// wsClient 表示一个 WebSocket 连接客户端。
// 使用 Server-Sent Events (SSE) 替代 WebSocket，避免引入第三方 WebSocket 库。
// SSE 是 HTTP 标准的一部分，浏览器原生支持，且为单向推送场景的最佳选择。
type wsClient struct {
	id     uint64
	events chan []byte
	done   chan struct{}
}

// EventHub 管理 SSE 客户端连接和事件广播。
//
// 使用 Server-Sent Events (SSE) 实现实时事件推送，
// 相比 WebSocket 的优势：
//   - 无需第三方依赖（标准 HTTP 协议）
//   - 浏览器原生支持 EventSource API
//   - 自动重连机制
//   - 适合单向推送场景（服务端 → 客户端）
//
// 线程安全：所有公共方法均可被多个 goroutine 安全调用。
type EventHub struct {
	mu        sync.RWMutex
	clients   map[uint64]*wsClient
	idCounter uint64
	logger    *slog.Logger

	// bufferSize 是每个客户端的事件缓冲区大小。
	bufferSize int
}

// NewEventHub 创建一个新的事件中心。
func NewEventHub(logger *slog.Logger) *EventHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventHub{
		clients:    make(map[uint64]*wsClient),
		logger:     logger,
		bufferSize: 256,
	}
}

// Subscribe 注册一个新的 SSE 客户端，返回客户端 ID 和事件 channel。
func (h *EventHub) Subscribe() (uint64, <-chan []byte, chan struct{}) {
	h.mu.Lock()
	h.idCounter++
	id := h.idCounter
	client := &wsClient{
		id:     id,
		events: make(chan []byte, h.bufferSize),
		done:   make(chan struct{}),
	}
	h.clients[id] = client
	h.mu.Unlock()

	h.logger.Debug("SSE client connected", "client_id", id)
	return id, client.events, client.done
}

// Unsubscribe 移除一个 SSE 客户端。
func (h *EventHub) Unsubscribe(id uint64) {
	h.mu.Lock()
	client, ok := h.clients[id]
	if ok {
		close(client.done)
		delete(h.clients, id)
	}
	h.mu.Unlock()

	if ok {
		h.logger.Debug("SSE client disconnected", "client_id", id)
	}
}

// Broadcast 向所有连接的客户端广播事件。
func (h *EventHub) Broadcast(event *WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("failed to marshal event", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.events <- data:
		default:
			// 缓冲区满，丢弃事件（避免阻塞）
			h.logger.Warn("SSE client buffer full, dropping event",
				"client_id", client.id,
				"event_type", event.Type,
			)
		}
	}
}

// ClientCount 返回当前连接的客户端数量。
func (h *EventHub) ClientCount() int {
	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	return n
}

// Close 关闭所有客户端连接。
func (h *EventHub) Close() {
	h.mu.Lock()
	for id, client := range h.clients {
		close(client.done)
		delete(h.clients, id)
	}
	h.mu.Unlock()
}

// ============================================================================
// Signal → SSE 事件桥接
// ============================================================================

// SignalBridge 将框架 Signal 系统的事件桥接到 EventHub。
//
// 通过注册 Signal 处理器，将 Spider 生命周期事件实时推送给所有 SSE 客户端。
type SignalBridge struct {
	hub    *EventHub
	logger *slog.Logger
}

// NewSignalBridge 创建一个新的信号桥接器。
func NewSignalBridge(hub *EventHub, logger *slog.Logger) *SignalBridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalBridge{
		hub:    hub,
		logger: logger,
	}
}

// RegisterSignals 在指定的 Signal Manager 上注册所有需要监听的信号。
// 返回注册的处理器 ID 列表（用于后续 Disconnect）。
func (b *SignalBridge) RegisterSignals(sm *signal.Manager, spiderID, spiderName string) []uint64 {
	signals := []signal.Signal{
		signal.SpiderOpened,
		signal.SpiderClosed,
		signal.SpiderError,
		signal.SpiderIdle,
		signal.ItemScraped,
		signal.ItemDropped,
		signal.ItemError,
		signal.RequestScheduled,
		signal.RequestDropped,
		signal.ResponseReceived,
		signal.ResponseDownloaded,
		signal.EngineStarted,
		signal.EngineStopped,
	}

	ids := make([]uint64, 0, len(signals))
	for _, sig := range signals {
		sigCopy := sig
		id := sm.Connect(func(params map[string]any) error {
			b.hub.Broadcast(&WSEvent{
				Type:       sigCopy.String(),
				SpiderID:   spiderID,
				SpiderName: spiderName,
				Timestamp:  time.Now(),
				Data:       sanitizeParams(params),
			})
			return nil
		}, sigCopy)
		ids = append(ids, id)
	}

	return ids
}

// sanitizeParams 清理信号参数，移除不可序列化的值。
func sanitizeParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}

	result := make(map[string]any, len(params))
	for k, v := range params {
		switch val := v.(type) {
		case string, int, int64, float64, bool, nil:
			result[k] = val
		case time.Duration:
			result[k] = val.String()
		case time.Time:
			result[k] = val.Format(time.RFC3339)
		case error:
			result[k] = val.Error()
		case map[string]any:
			result[k] = sanitizeParams(val)
		default:
			// 对于复杂类型，尝试 JSON 序列化
			if data, err := json.Marshal(val); err == nil {
				var parsed any
				if json.Unmarshal(data, &parsed) == nil {
					result[k] = parsed
				}
			}
		}
	}
	return result
}

// ============================================================================
// SSE HTTP Handler
// ============================================================================

// handleSSE 处理 GET /api/events — Server-Sent Events 端点。
//
// 客户端通过 EventSource API 连接此端点，接收实时爬虫事件推送。
//
// 事件格式：
//
//	data: {"type":"item_scraped","spider_id":"quotes-1","spider_name":"quotes","timestamp":"...","data":{...}}
//
// 支持的事件类型：
//   - spider_opened: Spider 启动
//   - spider_closed: Spider 关闭
//   - spider_error: Spider 回调错误
//   - item_scraped: Item 成功处理
//   - item_dropped: Item 被丢弃
//   - request_scheduled: 请求入队
//   - response_received: 响应接收
//   - engine_started: 引擎启动
//   - engine_stopped: 引擎停止
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// 检查是否支持 Flusher 接口（SSE 必需）
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// 订阅事件
	clientID, events, done := s.hub.Subscribe()
	defer s.hub.Unsubscribe(clientID)

	// 发送初始连接确认事件
	connectEvent, _ := json.Marshal(&WSEvent{
		Type:      "connected",
		Timestamp: time.Now(),
		Data: map[string]any{
			"client_id": clientID,
			"message":   "SSE connection established",
		},
	})
	_, _ = w.Write([]byte("data: " + string(connectEvent) + "\n\n"))
	flusher.Flush()

	// 事件循环
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case data := <-events:
			_, err := w.Write([]byte("data: " + string(data) + "\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleSSEStats 处理 GET /api/events/stats — 获取 SSE 连接统计。
func (s *Server) handleSSEStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Data: map[string]any{
			"connected_clients": s.hub.ClientCount(),
		},
	})
}

// ============================================================================
// 定时统计推送
// ============================================================================

// startStatsBroadcast 启动定时统计广播（每 2 秒推送一次运行中 Spider 的统计快照）。
func (s *Server) startStatsBroadcast(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.hub.ClientCount() == 0 {
				continue
			}

			allStats := s.getAllStats()
			if len(allStats) == 0 {
				continue
			}

			s.hub.Broadcast(&WSEvent{
				Type:      "stats_update",
				Timestamp: time.Now(),
				Data: map[string]any{
					"spiders": allStats,
				},
			})
		}
	}
}
