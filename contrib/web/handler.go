package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// apiResponse 是统一的 API 响应结构。
type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// registerRoutes 注册所有 REST API 路由。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spiders", s.handleListSpiders)
	mux.HandleFunc("POST /api/spiders/register", s.handleRegisterSpider)
	mux.HandleFunc("DELETE /api/spiders/{name}", s.handleUnregisterSpider)
	mux.HandleFunc("POST /api/spiders/{name}/start", s.handleStartSpider)
	mux.HandleFunc("POST /api/spiders/{name}/stop", s.handleStopSpider)
	mux.HandleFunc("GET /api/spiders/{name}/stats", s.handleGetStats)

	// 健康检查端点
	mux.HandleFunc("GET /api/health", s.handleHealth)
}

// handleListSpiders 处理 GET /api/spiders — 获取已注册的 Spider 列表及运行状态。
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "ok",
//	  "data": [
//	    {"name": "quotes", "running_instances": 1},
//	    {"name": "books", "running_instances": 0}
//	  ]
//	}
func (s *Server) handleListSpiders(w http.ResponseWriter, r *http.Request) {
	spiders := s.listSpiders()
	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Data:    spiders,
	})
}

// startSpiderRequest 是启动 Spider 的请求体。
type startSpiderRequest struct {
	// Settings 是可选的 Spider 级别配置覆盖（预留，Phase 1 暂不实现）
	Settings map[string]any `json:"settings,omitempty"`
}

// startSpiderResponse 是启动 Spider 的响应数据。
type startSpiderResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartTime time.Time `json:"start_time"`
}

// handleStartSpider 处理 POST /api/spiders/{name}/start — 按名称启动一个 Spider。
//
// 路径参数：
//   - name: Spider 注册名称
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "spider started",
//	  "data": {"id": "quotes-1", "name": "quotes", "start_time": "2026-05-13T12:00:00Z"}
//	}
func (s *Server) handleStartSpider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "spider name is required",
		})
		return
	}

	// 检查 Spider 是否已注册
	if !s.registry.Has(name) {
		writeJSON(w, http.StatusNotFound, apiResponse{
			Code:    http.StatusNotFound,
			Message: "spider not registered: " + name,
		})
		return
	}

	id, err := s.startSpider(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{
			Code:    http.StatusInternalServerError,
			Message: "failed to start spider: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "spider started",
		Data: startSpiderResponse{
			ID:        id,
			Name:      name,
			StartTime: time.Now(),
		},
	})
}

// handleStopSpider 处理 POST /api/spiders/{name}/stop — 按名称停止正在运行的 Spider。
//
// 路径参数：
//   - name: Spider 注册名称
//
// 查询参数（可选）：
//   - id: 指定停止某个运行实例（不指定则停止该名称的所有实例）
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "stopped 1 instance(s) of spider \"quotes\""
//	}
func (s *Server) handleStopSpider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "spider name is required",
		})
		return
	}

	// 如果指定了 id，停止特定实例
	if id := r.URL.Query().Get("id"); id != "" {
		if err := s.stopSpider(id); err != nil {
			writeJSON(w, http.StatusNotFound, apiResponse{
				Code:    http.StatusNotFound,
				Message: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{
			Code:    http.StatusOK,
			Message: "stop requested for spider instance: " + id,
		})
		return
	}

	// 否则停止该名称的所有实例
	count, err := s.stopSpiderByName(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiResponse{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: formatStopMessage(name, count),
	})
}

// handleGetStats 处理 GET /api/spiders/{name}/stats — 获取指定 Spider 的统计数据。
//
// 路径参数：
//   - name: Spider 注册名称
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "ok",
//	  "data": [
//	    {
//	      "id": "quotes-1",
//	      "name": "quotes",
//	      "start_time": "2026-05-13T12:00:00Z",
//	      "running": true,
//	      "stats": {"item_scraped_count": 42, "request_count": 100}
//	    }
//	  ]
//	}
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "spider name is required",
		})
		return
	}

	stats, err := s.getSpiderStats(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiResponse{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		})
		return
	}

	// 没有运行中的实例，但 Spider 已注册
	if stats == nil {
		stats = []SpiderStats{}
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Data:    stats,
	})
}

// ============================================================================
// 声明式 Spider 注册（预留接口 — Phase 1.5 实现）
// ============================================================================

// handleRegisterSpider 处理 POST /api/spiders/register — 通过声明式配置动态注册 Spider。
//
// 接收 JSON 格式的 SpiderSpec 配置，将其转换为 CrawlSpider 工厂函数并注册到 Registry。
// 无需编写 Go 代码，通过 REST API 即可创建基于规则的爬虫。
//
// 请求体示例：
//
//	{
//	  "name": "quotes",
//	  "start_urls": ["https://quotes.toscrape.com"],
//	  "allowed_domains": ["quotes.toscrape.com"],
//	  "rules": [
//	    {
//	      "link_extractor": {
//	        "allow": ["/page/\\d+"],
//	        "restrict_css": ["li.next a"]
//	      },
//	      "follow": true
//	    },
//	    {
//	      "link_extractor": {
//	        "allow": ["/author/"],
//	        "deny": ["/login"]
//	      },
//	      "callback": "parse_detail",
//	      "follow": false
//	    }
//	  ],
//	  "item_schemas": {
//	    "parse_detail": {
//	      "title": {"css": "h1::text"},
//	      "content": {"xpath": "//div[@class='content']/text()"},
//	      "url": {"value": "_response_url"}
//	    }
//	  },
//	  "settings": {
//	    "CONCURRENT_REQUESTS": 8,
//	    "DOWNLOAD_DELAY": "500ms"
//	  }
//	}
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "spider registered",
//	  "data": {"name": "quotes", "type": "declarative"}
//	}
//
// Phase 1.5 预留端点：当前返回 501 Not Implemented。
// 完整实现将在声明式 Spider 配置功能（P5-005h~j）中交付。
func (s *Server) handleRegisterSpider(w http.ResponseWriter, r *http.Request) {
	// 解析请求体，验证 SpiderSpec 格式
	var spec SpiderSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "invalid request body: " + err.Error(),
		})
		return
	}

	// 基本校验
	if spec.Name == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "spider name is required",
		})
		return
	}
	if len(spec.StartURLs) == 0 {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "at least one start_url is required",
		})
		return
	}

	// TODO(P5-005h): 将 SpiderSpec 转换为 CrawlSpider 工厂函数并注册
	// factory := spec.ToFactory()
	// s.registry.Register(spec.Name, factory)

	writeJSON(w, http.StatusNotImplemented, apiResponse{
		Code:    http.StatusNotImplemented,
		Message: "declarative spider registration is not yet implemented (planned for P5-005h)",
		Data: map[string]any{
			"name":          spec.Name,
			"start_urls":    spec.StartURLs,
			"rules_count":   len(spec.Rules),
			"schemas_count": len(spec.ItemSchemas),
		},
	})
}

// handleUnregisterSpider 处理 DELETE /api/spiders/{name} — 注销已注册的 Spider。
//
// 路径参数：
//   - name: Spider 注册名称
//
// 注意：如果该 Spider 有正在运行的实例，注销不会停止它们，
// 但后续无法再通过 start 端点启动新实例。
//
// 响应示例：
//
//	{
//	  "code": 200,
//	  "message": "spider unregistered: quotes"
//	}
func (s *Server) handleUnregisterSpider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "spider name is required",
		})
		return
	}

	if !s.registry.Has(name) {
		writeJSON(w, http.StatusNotFound, apiResponse{
			Code:    http.StatusNotFound,
			Message: "spider not registered: " + name,
		})
		return
	}

	s.registry.Unregister(name)
	s.logger.Info("spider unregistered", "name", name)

	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "spider unregistered: " + name,
	})
}

// handleHealth 处理 GET /api/health — 健康检查端点。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apiResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Data: map[string]any{
			"status":             "healthy",
			"registered_spiders": s.registry.Len(),
			"running_spiders":    len(s.getAllStats()),
		},
	})
}

// ============================================================================
// 辅助函数
// ============================================================================

// writeJSON 将 JSON 响应写入 http.ResponseWriter。
func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// 编码失败时记录日志（此时 header 已发送，无法更改状态码）
		http.Error(w, `{"code":500,"message":"internal error"}`, http.StatusInternalServerError)
	}
}

// formatStopMessage 格式化停止消息。
func formatStopMessage(name string, count int) string {
	if count == 1 {
		return fmt.Sprintf("stopped 1 instance of spider %q", name)
	}
	return fmt.Sprintf("stopped %d instances of spider %q", count, name)
}
