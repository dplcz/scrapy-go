// Package telemetry 提供 scrapy-go 框架的可观测性具体实现。
//
// 本包是 scrapy-go 框架的可插拔扩展模块（contrib），作为独立的 Go 子模块发布，
// 主模块 go.mod 不引入 OpenTelemetry / Prometheus 相关依赖，实现零侵入的可插拔设计。
//
// # 核心组件
//
//   - otel.Tracer：OpenTelemetry Tracer 适配器，实现 pkg/telemetry.Tracer 接口
//   - prometheus.Registry：Prometheus MetricsRegistry 适配器，实现 pkg/telemetry.MetricsRegistry 接口
//   - TraceExtension：信号驱动的分布式追踪扩展，自动为 Spider 生命周期和 HTTP 请求创建 Span
//   - MetricsExtension：信号驱动的指标收集扩展，自动收集请求/响应/Item/错误指标，含 HTTP /metrics 端点
//
// # 使用方式
//
// 通过 crawler.AddExtension 注入可观测性扩展：
//
//	import (
//	    "github.com/dplcz/scrapy-go/contrib/telemetry"
//	    "github.com/dplcz/scrapy-go/contrib/telemetry/otel"
//	    "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
//	    "github.com/dplcz/scrapy-go/pkg/crawler"
//	)
//
//	// 创建 OpenTelemetry Tracer 适配器
//	tp := initTracerProvider() // 用户自行初始化 OTel TracerProvider
//	tracer := otel.NewTracer(tp)
//
//	// 创建 Prometheus MetricsRegistry 适配器
//	registry := prometheus.NewRegistry()
//
//	// 创建并注入扩展
//	c := crawler.NewDefault()
//	traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger)
//	metricsExt := telemetry.NewMetricsExtension(registry, ":9090", c.Signals, c.Logger)
//	c.AddExtension(traceExt, "TraceExtension", 100)
//	c.AddExtension(metricsExt, "MetricsExtension", 101)
//
// # 设计决策
//
//   - 适配器模式：将 OTel/Prometheus 的具体实现适配为 pkg/telemetry 定义的轻量级接口
//   - 信号驱动：通过框架信号系统（Signal）自动采集数据，无需修改业务代码
//   - 独立子模块：避免主模块引入重量级依赖，用户按需安装
//   - HTTP /metrics 端点：MetricsExtension 内置 Prometheus HTTP 端点，开箱即用
package telemetry
