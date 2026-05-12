# 🚀 scrapy-go 用户指南（Getting Started）

本指南将带你从零开始使用 scrapy-go 框架构建一个完整的网页爬虫。

---

## 📑 目录

- [环境要求](#环境要求)
- [安装](#安装)
- [创建项目](#创建项目)
- [编写第一个 Spider](#编写第一个-spider)
- [运行爬虫](#运行爬虫)
- [使用选择器提取数据](#使用选择器提取数据)
- [跟踪链接](#跟踪链接)
- [使用 Item Pipeline 处理数据](#使用-item-pipeline-处理数据)
- [数据导出（Feed Export）](#数据导出feed-export)
- [使用 CrawlSpider 自动爬取](#使用-crawlspider-自动爬取)
- [配置说明](#配置说明)
- [中间件](#中间件)
- [多爬虫运行](#多爬虫运行)
- [优雅关闭](#优雅关闭)
- [调试与性能分析](#调试与性能分析)
- [下一步](#下一步)

---

## 环境要求

- **Go 1.21+**（推荐 Go 1.22+）
- 操作系统：Linux / macOS / Windows

---

## 安装

### 方式一：作为库引入

```bash
go get github.com/dplcz/scrapy-go@latest
```

### 方式二：安装 CLI 工具（项目脚手架）

```bash
go install github.com/dplcz/scrapy-go/cmd/scrapy-go@latest
```

验证安装：

```bash
scrapy-go version
```

---

## 创建项目

使用 CLI 工具快速创建项目骨架：

```bash
scrapy-go startproject myproject
```

生成的项目结构：

```
myproject/
├── main.go              # 入口文件
├── project/             # 项目级组件
│   ├── settings.go      # 项目配置
│   ├── middlewares.go   # 自定义中间件
│   ├── pipelines.go     # 自定义 Pipeline
│   └── items.go         # Item 定义
├── spiders/             # 爬虫目录
│   └── .gitkeep
├── go.mod               # Go 模块文件
└── scrapy-go.toml       # 框架配置文件
```

也可以不使用脚手架，直接在任意 Go 项目中引入 scrapy-go：

```bash
mkdir myspider && cd myspider
go mod init myspider
go get github.com/dplcz/scrapy-go@latest
```

---

## 编写第一个 Spider

Spider 是 scrapy-go 的核心概念，定义了如何爬取网站和提取数据。

### 最简示例

创建 `main.go`：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/dplcz/scrapy-go/pkg/crawler"
    shttp "github.com/dplcz/scrapy-go/pkg/http"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

// QuotesSpider 爬取 quotes.toscrape.com 的引用数据。
type QuotesSpider struct {
    spider.Base // 嵌入 Base 获得默认实现
}

// Parse 解析响应，提取引用数据。
func (s *QuotesSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    // 使用 CSS 选择器提取数据
    quotes := response.CSS("div.quote")
    for _, q := range quotes {
        text := q.CSS("span.text::text").Get("")
        author := q.CSS("small.author::text").Get("")
        fmt.Printf("Quote: %s\n  — %s\n\n", text, author)
    }
    return nil, nil
}

func main() {
    sp := &QuotesSpider{
        Base: spider.Base{
            SpiderName: "quotes",
            StartURLs:  []string{"https://quotes.toscrape.com/"},
        },
    }

    c := crawler.NewDefault()
    if err := c.Run(context.Background(), sp); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}
```

### 关键概念

| 概念 | 说明 |
|------|------|
| `spider.Base` | Spider 基类，提供 `Name()`、`Start()`、`Closed()` 等默认实现 |
| `SpiderName` | 爬虫唯一名称 |
| `StartURLs` | 初始 URL 列表，框架自动为每个 URL 创建 GET 请求 |
| `Parse()` | 默认回调函数，处理每个响应 |
| `spider.Output` | 回调返回值，可以是新请求（`Output{Request: req}`）或数据项（`Output{Item: item}`） |

---

## 运行爬虫

```bash
go run main.go
```

输出示例：

```
2026/05/08 10:00:00 INFO spider opened spider=quotes
Quote: "The world as we have created it is a process of our thinking."
  — Albert Einstein

Quote: "It is our choices, Harry, that show what we truly are."
  — J.K. Rowling
...
2026/05/08 10:00:01 INFO spider closed reason=finished
```

---

## 使用选择器提取数据

scrapy-go 内置两种选择器引擎：

### CSS 选择器（基于 goquery）

```go
// 选择所有匹配的元素
items := response.CSS("div.product")

// 提取文本内容
title := response.CSS("h1::text").Get("")

// 提取属性
href := response.CSS("a.next::attr(href)").Get("")

// 获取所有匹配的文本
allTags := response.CSS("a.tag::text").GetAll()
```

### XPath 选择器（基于 htmlquery）

```go
// XPath 选择
title := response.XPath("//h1/text()").Get("")

// 带条件的 XPath
author := response.XPath(`//span[@class="author"]/text()`).Get("")

// 获取属性
link := response.XPath("//a/@href").Get("")
```

### 链式选择

选择器支持链式调用，在子元素中继续查找：

```go
for _, product := range response.CSS("div.product") {
    name := product.CSS("h2::text").Get("")
    price := product.CSS("span.price::text").Get("")
    link := product.CSS("a::attr(href)").Get("")
    
    fmt.Printf("Product: %s, Price: %s\n", name, price)
}
```

---

## 跟踪链接

通过在 `Parse` 中返回新的 Request，实现链接跟踪：

```go
func (s *QuotesSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    var outputs []spider.Output

    // 提取数据
    for _, q := range response.CSS("div.quote") {
        item := map[string]any{
            "text":   q.CSS("span.text::text").Get(""),
            "author": q.CSS("small.author::text").Get(""),
        }
        outputs = append(outputs, spider.Output{Item: item})
    }

    // 跟踪下一页链接
    nextPage := response.CSS("li.next a::attr(href)").Get("")
    if nextPage != "" {
        nextURL := response.URLJoin(nextPage)
        req, err := shttp.NewRequest(nextURL)
        if err == nil {
            outputs = append(outputs, spider.Output{Request: req})
        }
    }

    return outputs, nil
}
```

### 使用自定义回调

```go
func (s *MySpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    var outputs []spider.Output

    for _, link := range response.CSS("a.detail::attr(href)").GetAll() {
        detailURL := response.URLJoin(link)
        req, _ := shttp.NewRequest(detailURL,
            shttp.WithCallback(s.ParseDetail), // 指定回调函数
        )
        outputs = append(outputs, spider.Output{Request: req})
    }

    return outputs, nil
}

func (s *MySpider) ParseDetail(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    item := map[string]any{
        "title":   response.CSS("h1::text").Get(""),
        "content": response.CSS("div.content::text").Get(""),
    }
    return []spider.Output{{Item: item}}, nil
}
```

---

## 使用 Item Pipeline 处理数据

Item Pipeline 用于处理 Spider 产出的数据项（清洗、验证、存储等）。

### 定义 Pipeline

```go
// JsonFilePipeline 将 Item 写入 JSON 文件。
type JsonFilePipeline struct {
    file    *os.File
    encoder *json.Encoder
}

// Open 在 Spider 打开时调用。
func (p *JsonFilePipeline) Open(ctx context.Context) error {
    f, err := os.Create("output.json")
    if err != nil {
        return err
    }
    p.file = f
    p.encoder = json.NewEncoder(f)
    return nil
}

// ProcessItem 处理每个 Item。
func (p *JsonFilePipeline) ProcessItem(ctx context.Context, item any) (any, error) {
    return item, p.encoder.Encode(item)
}

// Close 在 Spider 关闭时调用。
func (p *JsonFilePipeline) Close(ctx context.Context) error {
    if p.file != nil {
        return p.file.Close()
    }
    return nil
}
```

### 注册 Pipeline

```go
c := crawler.NewDefault()
c.AddPipeline(&JsonFilePipeline{}, "JsonFile", 300) // priority 越小越先执行
c.Run(ctx, mySpider)
```

---

## 数据导出（Feed Export）

scrapy-go 内置 Feed Export 系统，支持多种格式导出：

### 支持的格式

| 格式 | 说明 |
|------|------|
| JSON | 标准 JSON 数组 |
| JSON Lines | 每行一个 JSON 对象 |
| CSV | 逗号分隔值 |
| XML | XML 文档 |

### 使用方式

```go
c := crawler.NewDefault()

// 导出为 JSON Lines 文件
c.AddFeed(feedexport.FeedConfig{
    URI:    "file:///tmp/output.jsonl",
    Format: feedexport.FormatJSONLines,
})

// 导出为 CSV 文件
c.AddFeed(feedexport.FeedConfig{
    URI:    "file:///tmp/output.csv",
    Format: feedexport.FormatCSV,
})

c.Run(ctx, mySpider)
```

### 使用 Struct Item + FieldMeta

```go
// 定义结构化 Item
type Product struct {
    Name     string   `item:"name"`
    Price    float64  `item:"price"`
    Category string   `item:"category"`
    Tags     []string `item:"tags"`
}

// 在 Spider 中返回结构化 Item
func (s *MySpider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    product := Product{
        Name:  resp.CSS("h1::text").Get(""),
        Price: 29.99,
    }
    return []spider.Output{{Item: product}}, nil
}
```

---

## 使用 CrawlSpider 自动爬取

`CrawlSpider` 通过规则（Rule）自动提取和跟踪链接，无需手动编写链接跟踪逻辑：

```go
type ArticleSpider struct {
    spider.CrawlSpider
}

func NewArticleSpider() *ArticleSpider {
    s := &ArticleSpider{}
    s.SpiderName = "articles"
    s.StartURLs = []string{"https://example.com/"}

    s.Rules = []spider.Rule{
        // 规则 1：跟踪分类页面（不提取数据）
        {
            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
                linkextractor.WithAllow(`/category/`),
            ),
            // Callback 为 nil，Follow 默认为 true
        },
        // 规则 2：提取文章页面数据
        {
            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
                linkextractor.WithAllow(`/article/\d+`),
                linkextractor.WithDeny(`/article/\d+/comments`),
            ),
            Callback: s.parseArticle,
        },
    }

    return s
}

func (s *ArticleSpider) parseArticle(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    item := map[string]any{
        "title":   resp.CSS("h1::text").Get(""),
        "author":  resp.CSS(".author::text").Get(""),
        "content": resp.CSS("article p::text").Get(""),
    }
    return []spider.Output{{Item: item}}, nil
}
```

### Rule 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `LinkExtractor` | `linkextractor.LinkExtractor` | 链接提取器，定义从页面中提取哪些链接 |
| `Callback` | `spider.CallbackFunc` | 匹配链接的响应回调，nil 表示仅跟踪不提取 |
| `Follow` | `bool` | 是否继续从匹配页面提取链接（Callback 为 nil 时默认 true） |
| `ProcessLinks` | `func([]Link) []Link` | 链接后处理钩子 |
| `ProcessRequest` | `func(*Request) *Request` | 请求后处理钩子 |

---

## 配置说明

### Spider 级别配置

通过 `CustomSettings()` 方法返回 Spider 专属配置：

```go
func (s *MySpider) CustomSettings() *spider.Settings {
    return &spider.Settings{
        ConcurrentRequests:          spider.IntPtr(8),
        ConcurrentRequestsPerDomain: spider.IntPtr(4),
        DownloadDelay:               spider.DurationPtr(time.Second),
        DownloadTimeout:             spider.DurationPtr(30 * time.Second),
        RetryTimes:                  spider.IntPtr(3),
        UserAgent:                   spider.StringPtr("MyBot/1.0"),
        LogLevel:                    spider.StringPtr("INFO"),
    }
}
```

### 全局配置（TOML 文件）

`scrapy-go.toml` 示例：

```toml
[scrapy]
concurrent_requests = 16
concurrent_requests_per_domain = 8
download_delay = "0s"
download_timeout = "180s"
retry_times = 2
log_level = "INFO"
user_agent = "scrapy-go/1.0"

[scrapy.retry]
enabled = true
http_codes = [500, 502, 503, 504, 408, 429]

[scrapy.redirect]
enabled = true
max_times = 20
```

### 配置优先级

从低到高：

1. **Default** — 框架内置默认值
2. **Command** — 命令行参数
3. **Addon** — 插件配置
4. **Project** — 项目级配置（TOML 文件 / `settings.go`）
5. **Spider** — Spider 的 `CustomSettings()` 返回值
6. **Request** — 请求级别的 `meta` 配置

### 常用配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `CONCURRENT_REQUESTS` | 16 | 全局最大并发请求数 |
| `CONCURRENT_REQUESTS_PER_DOMAIN` | 8 | 每域名最大并发 |
| `DOWNLOAD_DELAY` | 0 | 下载延迟 |
| `DOWNLOAD_TIMEOUT` | 180s | 下载超时 |
| `RETRY_ENABLED` | true | 是否启用重试 |
| `RETRY_TIMES` | 2 | 最大重试次数 |
| `REDIRECT_ENABLED` | true | 是否启用重定向 |
| `REDIRECT_MAX_TIMES` | 20 | 最大重定向次数 |
| `DEPTH_LIMIT` | 0 | 爬取深度限制（0=无限） |
| `LOG_LEVEL` | DEBUG | 日志级别 |
| `USER_AGENT` | scrapy-go | User-Agent 字符串 |
| `ROBOTSTXT_OBEY` | false | 是否遵守 robots.txt |
| `AUTOTHROTTLE_ENABLED` | false | 是否启用自适应限速 |
| `CIRCUIT_BREAKER_ENABLED` | false | 是否启用域名级熔断器 |

---

## 中间件

### 内置下载器中间件

scrapy-go 提供以下内置下载器中间件（按默认优先级排序）：

| 中间件 | 优先级 | 功能 |
|--------|--------|------|
| RobotsTxt | 100 | 遵守 robots.txt 规则 |
| HttpAuth | 300 | HTTP Basic 认证 |
| DownloadTimeout | 350 | 下载超时控制 |
| UserAgent | 400 | 设置 User-Agent |
| Retry | 500 | 失败重试（支持指数退避 + 抖动 + 差异化策略） |
| CircuitBreaker | 545 | 域名级熔断器（连续失败自动熔断） |
| DefaultHeaders | 550 | 设置默认请求头 |
| Redirect | 600 | HTTP 重定向处理 |
| Cookies | 700 | Cookie 管理 |
| HttpCompression | 750 | 响应解压（gzip/br/deflate） |
| HttpProxy | 800 | HTTP 代理支持 |
| Stats | 850 | 下载统计 |
| HttpCache | 900 | HTTP 缓存 |

### 自定义下载器中间件

```go
// ProxyRotator 实现代理轮换。
type ProxyRotator struct {
    proxies []string
    index   int
}

// ProcessRequest 在请求发送前设置代理。
func (p *ProxyRotator) ProcessRequest(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
    proxy := p.proxies[p.index%len(p.proxies)]
    req.Meta["proxy"] = proxy
    p.index++
    return nil, nil // 返回 nil 表示继续处理链
}

// 注册中间件
c := crawler.NewDefault()
c.AddDownloaderMiddleware(&ProxyRotator{
    proxies: []string{"http://proxy1:8080", "http://proxy2:8080"},
}, "ProxyRotator", 750)
```

### 内置 Spider 中间件

| 中间件 | 功能 |
|--------|------|
| Depth | 爬取深度控制 |
| HttpError | HTTP 错误过滤（非 2xx 响应） |
| Offsite | 域外请求过滤 |
| Referer | 自动设置 Referer 头 |
| URLLength | URL 长度限制 |

---

## 多爬虫运行

使用 `Runner` 并发或顺序运行多个 Spider：

### 并发运行

```go
runner := crawler.NewRunner()
err := runner.StartConcurrent(ctx,
    crawler.NewJob(crawler.New(), spiderA),
    crawler.NewJob(crawler.New(), spiderB),
    crawler.NewJob(crawler.New(), spiderC),
)
```

### 顺序运行

```go
runner := crawler.NewRunner()
err := runner.StartSequentially(ctx,
    crawler.NewJob(crawler.New(), spiderA),
    crawler.NewJob(crawler.New(), spiderB),
)
```

---

## 优雅关闭

scrapy-go 支持两阶段信号处理：

1. **第一次 SIGINT/SIGTERM** — 触发优雅关闭：
   - 停止取新请求
   - 等待 in-flight 请求完成
   - 等待 Pipeline 排空
   - 关闭所有组件

2. **第二次 SIGINT/SIGTERM** — 强制退出进程

也可以通过代码控制：

```go
c := crawler.NewDefault()

// 在另一个 goroutine 中停止爬虫
go func() {
    time.Sleep(30 * time.Second)
    c.Stop() // 触发优雅关闭
}()

c.Run(ctx, mySpider)
```

或使用 context 超时：

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

c := crawler.NewDefault()
c.Run(ctx, mySpider) // 5 分钟后自动停止
```

---

## 调试与性能分析

### 启用 pprof

```go
import "github.com/dplcz/scrapy-go/pkg/debug"

// 启用 pprof HTTP 端点（默认 :6060）
c := crawler.New()
c.AddExtension(debug.NewPprofExtension(":6060"), "Pprof", 0)
c.Run(ctx, mySpider)
```

访问 `http://localhost:6060/debug/pprof/` 查看运行时分析数据。

### 统计信息

爬取完成后，框架自动输出统计信息：

```
2026/05/08 10:00:05 INFO  Dumping Scrapy stats:
  downloader/request_count: 150
  downloader/response_count: 148
  downloader/response_status_count/200: 145
  downloader/response_status_count/404: 3
  item_scraped_count: 500
  elapsed_time_seconds: 5.2
  finish_reason: finished
```

---

## 下一步

- 📖 [架构设计文档](../architecture/architecture.md) — 深入了解框架内部工作原理
- 🔄 [迁移指南](../migration/migration-from-python.md) — 从 Python Scrapy 迁移到 scrapy-go
- 📚 [API 参考](https://pkg.go.dev/github.com/dplcz/scrapy-go) — 完整的 godoc 文档
- 💡 [示例代码](../../examples/) — 更多实际使用示例
  - `examples/quotes/` — 基础爬虫示例
  - `examples/crawlspider/` — CrawlSpider 规则爬取
  - `examples/feedexport/` — 数据导出示例
  - `examples/custom_middleware/` — 自定义中间件
  - `examples/itemadapter/` — ItemAdapter 使用
