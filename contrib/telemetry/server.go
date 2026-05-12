package telemetry

import (
	"context"
	"net"
	"net/http"

	promreg "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// startHTTPServer 启动 HTTP /metrics 端点。
//
// 返回一个 stop 函数用于优雅关闭 HTTP 服务器。
// 仅当 MetricsRegistry 底层为 Prometheus Registry 时才启动。
func (e *MetricsExtension) startHTTPServer() (func(), error) {
	mux := http.NewServeMux()

	// 尝试获取 Prometheus Registry 用于 HTTP handler
	if pr, ok := e.registry.(*promreg.Registry); ok {
		mux.Handle("/metrics", promhttp.HandlerFor(pr.PrometheusRegistry(), promhttp.HandlerOpts{}))
	} else {
		// 非 Prometheus 后端，提供简单的健康检查端点
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("# metrics endpoint (non-prometheus backend)\n"))
		})
	}

	// 健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	listener, err := net.Listen("tcp", e.addr)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			e.logger.Error("metrics HTTP server error", "error", err)
		}
	}()

	stop := func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			e.logger.Error("failed to shutdown metrics HTTP server", "error", err)
		}
	}

	return stop, nil
}
