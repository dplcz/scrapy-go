# scrapy-go/contrib/web

> Scrapy-Go 全栈 Web 管理平台 — 提供 REST API、实时事件推送、Dashboard UI 和声明式爬虫创建。

## 功能概览

- **REST API**：Spider 注册、启动、停止、统计查询
- **SSE 实时事件推送**：基于 Signal 系统的 Spider 生命周期事件实时推送
- **Dashboard UI**：爬虫状态总览、统计图表、实时日志流、操作面板
- **声明式爬虫创建**：通过 JSON 配置创建 CrawlSpider，无需编写 Go 代码
- **爬取历史持久化**：运行记录、统计快照存储与查询

## 快速开始

```go
package main

import (
    "context"
    "log"

    "github.com/dplcz/scrapy-go/contrib/web"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

func main() {
    // 创建 Web 管理服务器
    srv := web.NewServer(":8080",
        web.WithStore(web.NewStore(web.WithStorePath("./history.json"))),
    )

    // 注册 Spider 工厂函数
    srv.Register("quotes", func() spider.Spider {
        return NewQuotesSpider()
    })

    // 启动服务器（访问 http://localhost:8080 查看 Dashboard）
    if err := srv.ListenAndServe(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## REST API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/spiders` | 获取已注册的 Spider 列表及运行状态 |
| POST | `/api/spiders/register` | 通过声明式配置动态注册 Spider |
| DELETE | `/api/spiders/{name}` | 注销已注册的 Spider |
| POST | `/api/spiders/{name}/start` | 启动 Spider（支持 args 参数注入） |
| POST | `/api/spiders/{name}/stop` | 停止 Spider |
| GET | `/api/spiders/{name}/stats` | 获取 Spider 统计数据 |
| GET | `/api/events` | SSE 实时事件推送端点 |
| GET | `/api/events/stats` | SSE 连接统计 |
| GET | `/api/history` | 获取爬取历史记录 |
| GET | `/api/history/{id}` | 获取单条历史记录 |
| GET | `/api/history/stats` | 获取历史统计汇总 |
| GET | `/api/health` | 健康检查 |
| GET | `/` | Dashboard Web UI |

## 声明式爬虫创建

通过 `POST /api/spiders/register` 提交 JSON 格式的 SpiderSpec 配置，无需编写 Go 代码即可创建基于规则的爬虫：

```json
{
  "name": "quotes",
  "start_urls": ["https://quotes.toscrape.com"],
  "allowed_domains": ["quotes.toscrape.com"],
  "rules": [
    {
      "link_extractor": {
        "allow": ["/page/\\d+"],
        "restrict_css": ["li.next a"]
      },
      "follow": true
    },
    {
      "link_extractor": {
        "allow": ["/author/"],
        "deny": ["/login"]
      },
      "callback": "parse_detail",
      "follow": false
    }
  ],
  "item_schemas": {
    "parse_detail": {
      "title": {"css": "h1::text"},
      "content": {"xpath": "//div[@class='content']/text()"},
      "url": {"value": "_response_url"}
    }
  },
  "settings": {
    "CONCURRENT_REQUESTS": 8,
    "DOWNLOAD_DELAY": "500ms"
  }
}
```

### SpiderSpec JSON Schema

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | Spider 唯一标识名称 |
| `start_urls` | []string | ✅ | 初始爬取 URL 列表 |
| `allowed_domains` | []string | | 允许爬取的域名列表 |
| `rules` | []RuleSpec | | 爬取规则列表 |
| `item_schemas` | map[string]ItemSchema | | 数据提取规则 |
| `settings` | map[string]any | | 配置覆盖 |

### RuleSpec

| 字段 | 类型 | 说明 |
|------|------|------|
| `link_extractor` | LinkExtractorSpec | 链接提取器配置 |
| `callback` | string | 回调名称（对应 item_schemas 的 key） |
| `follow` | *bool | 是否跟踪链接 |

### LinkExtractorSpec

| 字段 | 类型 | 说明 |
|------|------|------|
| `allow` | []string | 允许的 URL 正则表达式 |
| `deny` | []string | 拒绝的 URL 正则表达式 |
| `allow_domains` | []string | 允许的域名 |
| `deny_domains` | []string | 拒绝的域名 |
| `restrict_css` | []string | 限制提取范围的 CSS 选择器 |
| `restrict_xpath` | []string | 限制提取范围的 XPath |
| `tags` | []string | 要扫描的 HTML 标签（默认 a, area） |
| `attrs` | []string | 要扫描的 HTML 属性（默认 href） |

### FieldExtractor

| 字段 | 类型 | 说明 |
|------|------|------|
| `css` | string | CSS 选择器（支持 ::text, ::attr(name)） |
| `xpath` | string | XPath 表达式 |
| `value` | string | 特殊值（`_response_url`, `_timestamp`, 或字面量） |
| `regex` | string | 对提取结果进行正则匹配 |
| `default` | string | 提取失败时的默认值 |

## SSE 实时事件

通过 `EventSource` API 连接 `/api/events` 端点接收实时事件：

```javascript
const source = new EventSource('/api/events');
source.onmessage = (e) => {
    const event = JSON.parse(e.data);
    console.log(event.type, event.spider_name, event.data);
};
```

### 事件类型

| 类型 | 说明 |
|------|------|
| `connected` | SSE 连接建立 |
| `spider_started` | Spider 启动 |
| `spider_finished` | Spider 运行结束 |
| `spider_registered` | 声明式 Spider 注册 |
| `spider_opened` | Spider 打开（引擎级） |
| `spider_closed` | Spider 关闭（引擎级） |
| `spider_error` | Spider 回调错误 |
| `item_scraped` | Item 成功处理 |
| `item_dropped` | Item 被丢弃 |
| `request_scheduled` | 请求入队 |
| `response_received` | 响应接收 |
| `engine_started` | 引擎启动 |
| `engine_stopped` | 引擎停止 |
| `stats_update` | 定时统计快照（每 2 秒） |

## 设计决策

- **零外部依赖**：基于 Go 标准库 `net/http` 实现，不引入第三方 Web 框架
- **SSE 替代 WebSocket**：Server-Sent Events 是 HTTP 标准的一部分，浏览器原生支持，适合单向推送
- **嵌入式前端**：Dashboard UI 通过 `go:embed` 嵌入二进制，无需额外部署
- **声明式配置**：SpiderSpec → CrawlSpider 转换引擎，覆盖列表页→详情页模式
- **Runner 集成**：通过 `crawler.Runner` 管理多爬虫并发执行
- **信号桥接**：通过 `Runner.ConnectSignal` 安全地将框架 Signal 事件桥接到 SSE

## 版本历史

- **v1.2.5 (Phase 2)**：全栈 Web 平台 + 声明式爬虫创建
  - WebSocket(SSE) 实时事件推送
  - Dashboard UI（Vue.js 3 + Chart.js + Bootstrap 5）
  - 爬取历史持久化
  - SpiderSpec → CrawlSpider 转换引擎
  - POST /api/spiders/register 完整实现
  - 运行历史 API
- **v1.2.0 (Phase 1)**：轻量级 REST API
  - Spider 注册表 + 工厂模式
  - REST API handlers（list/start/stop/stats）
  - 集成测试（42 个测试，覆盖率 86.0%）
