// Package agent 定义了 scrapy-go Agent Daemon 与管理平台之间的通信接口。
//
// 本包仅包含接口定义和数据传输对象（DTO），不包含具体实现。
// 具体实现位于独立项目 scrapy-go-agent（Agent Daemon 服务端）
// 和 scrapy-go-web（多 Agent 中央管理平台）。
//
// # 架构概述
//
// scrapy-go 的分布式部署采用 Agent Daemon 模式（类似 Python 生态的 scrapyd）：
//
//   - Agent Daemon：每台机器上运行的独立 daemon 进程，通过 HTTP API 接收爬虫部署包、
//     启动/停止/监控爬虫子进程
//   - 管理平台：中央管理面板，连接多台机器的 Agent Daemon，统一下发任务、分发爬虫包、
//     聚合监控
//
// # 接口设计
//
//   - AgentServer：Agent Daemon 服务端接口，定义 daemon 需要实现的核心能力
//   - AgentClient：Agent Daemon 客户端接口，供管理平台调用 Agent 的 HTTP API
//   - HeartbeatReporter：心跳上报接口，Agent 定期向管理平台上报节点状态
//
// # 使用方式
//
// Agent Daemon 实现端（scrapy-go-agent）：
//
//	import "github.com/dplcz/scrapy-go/pkg/agent"
//
//	type Daemon struct { /* ... */ }
//
//	// 实现 AgentServer 接口
//	var _ agent.AgentServer = (*Daemon)(nil)
//
// 管理平台调用端（scrapy-go-web）：
//
//	import "github.com/dplcz/scrapy-go/pkg/agent"
//
//	// 通过 AgentClient 接口与远程 Agent 通信
//	client := agent.NewHTTPClient("http://agent-host:6800")
//	jobs, err := client.ListJobs(ctx)
//
// # 版本规划
//
//   - v1.3.0（M8）：接口定义与文档（本包）
//   - v1.4.0（M9）：Agent Daemon 核心实现（scrapy-go-agent 独立项目）
//   - v1.5.0（M10）：多 Agent 中央管理平台（scrapy-go-web 独立项目）
package agent
