# 🔄 从 Python Scrapy 迁移到 scrapy-go

本指南帮助熟悉 Python Scrapy 的开发者快速迁移到 scrapy-go。
通过对照表和代码示例，展示两个框架之间的概念映射和 API 差异。

---

## 📑 目录

- [概念映射总览](#概念映射总览)
- [项目结构对比](#项目结构对比)
- [Spider 迁移](#spider-迁移)
- [Request 与 Response](#request-与-response)
- [选择器](#选择器)
- [Item 定义](#item-定义)
- [Item Pipeline](#item-pipeline)
- [中间件迁移](#中间件迁移)
- [配置迁移](#配置迁移)
- [Feed Export](#feed-export)
- [信号系统](#信号系统)
- [CrawlSpider 与 Rule](#crawlspider-与-rule)
- [命令行工具](#命令行工具)
- [不支持的特性](#不支持的特性)
- [常见迁移模式](#常见迁移模式)
- [性能对比](#性能对比)

---

## 概念映射总览

| Python Scrapy | scrapy-go | 说明 |
|---------------|-----------|------|
| `scrapy.Spider` | `spider.Base` | 嵌入 Base 结构体 |
| `scrapy.CrawlSpider` | `spider.CrawlSpider` | 嵌入 CrawlSpider 结构体 |
| `scrapy.Request` | `shttp.Request` | `pkg/http` 包 |
| `scrapy.http.Response` | `shttp.Response` | `pkg/http` 包 |
| `scrapy.Item` / `dict` | `struct` + `item` tag / `map[string]any` | Go 类型系统 |
| `ItemPipeline` | `pipeline.ItemPipeline` | 接口实现 |
| `DownloaderMiddleware` | `dmiddle.DownloaderMiddleware` | 接口实现 |
| `SpiderMiddleware` | `smiddle.SpiderMiddleware` | 接口实现 |
| `CrawlerProcess` | `crawler.Crawler` | 单爬虫编排 |
| `CrawlerRunner` | `crawler.Runner` | 多爬虫调度 |
| `settings.py` | `spider.Settings` / `scrapy-go.toml` | 配置系统 |
| `FEED_EXPORT_*` | `crawler.AddFeed()` | Feed 导出 |
| `scrapy.signals` | `pkg/signal` | 信号系统 |
| `Extension` | `extension.Extension` | 扩展接口 |
| `scrapy startproject` | `scrapy-go startproject` | 项目脚手架 |
| `scrapy genspider` | `scrapy-go genspider` | Spider 生成 |
| `yield` | `return []spider.Output{...}` | 回调返回值 |
| `Deferred` / `async` | goroutine + channel | 异步模型 |
| `Twisted reactor` | `context.Context` | 生命周期管理 |

---

## 项目结构对比

### Python Scrapy

```
myproject/
├── scrapy.cfg
└── myproject/
    ├── __init__.py
    ├── items.py
    ├── middlewares.py
    ├── pipelines.py
    ├── settings.py
    └── spiders/
        ├── __init__.py
        └── myspider.py
```

### scrapy-go

```
myproject/
├── main.go
├── project/
│   ├── settings.go
│   ├── middlewares.go
│   ├── pipelines.go
│   └── items.go
├── spiders/
│   └── myspider.go
├── go.mod
└── scrapy-go.toml
```

### 关键差异

- Go 没有 `__init__.py`，使用包（package）组织代码
- 入口文件是 `main.go`（Go 需要 main 函数）
- 配置使用 TOML 格式替代 Python 模块

---

## Spider 迁移

### Python

```python
import scrapy

class QuotesSpider(scrapy.Spider):
    name = "quotes"
    start_urls = ["https://quotes.toscrape.com/"]
    
    custom_settings = {
        "CONCURRENT_REQUESTS": 8,
        "DOWNLOAD_DELAY": 1,
    }

    def parse(self, response):
        for quote in response.css("div.quote"):
            yield {
                "text": quote.css("span.text::text").get(),
                "author": quote.css("small.author::text").get(),
            }
        
        next_page = response.css("li.next a::attr(href)").get()
        if next_page:
            yield response.follow(next_page, self.parse)
```

### scrapy-go

```go
package main

import (
    "context"
    "time"

    "github.com/dplcz/scrapy-go/pkg/crawler"
    shttp "github.com/dplcz/scrapy-go/pkg/http"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

type QuotesSpider struct {
    spider.Base
}

func (s *QuotesSpider) CustomSettings() *spider.Settings {
    return &spider.Settings{
        ConcurrentRequests: spider.IntPtr(8),
        DownloadDelay:      spider.DurationPtr(time.Second),
    }
}

func (s *QuotesSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    var outputs []spider.Output

    for _, quote := range response.CSS("div.quote") {
        item := map[string]any{
            "text":   quote.CSS("span.text::text").Get(""),
            "author": quote.CSS("small.author::text").Get(""),
        }
        outputs = append(outputs, spider.Output{Item: item})
    }

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

func main() {
    sp := &QuotesSpider{
        Base: spider.Base{
            SpiderName: "quotes",
            StartURLs:  []string{"https://quotes.toscrape.com/"},
        },
    }
    c := crawler.NewDefault()
    c.Run(context.Background(), sp)
}
```

### 迁移要点

| Python | Go | 说明 |
|--------|-----|------|
| `name = "quotes"` | `SpiderName: "quotes"` | 结构体字段 |
| `start_urls = [...]` | `StartURLs: []string{...}` | 结构体字段 |
| `custom_settings = {...}` | `CustomSettings()` 方法 | 返回 `*spider.Settings` |
| `def parse(self, response):` | `func (s *T) Parse(ctx, resp)` | 方法签名 |
| `yield item` | `outputs = append(outputs, spider.Output{Item: item})` | 收集后统一返回 |
| `yield request` | `outputs = append(outputs, spider.Output{Request: req})` | 收集后统一返回 |
| `response.follow(url)` | `shttp.NewRequest(response.URLJoin(url))` | 显式构造 |
| `response.css(...)` | `response.CSS(...)` | 方法名大写 |

---

## Request 与 Response

### 创建 Request

**Python:**
```python
# 基本请求
yield scrapy.Request(url, callback=self.parse_detail)

# 带 meta
yield scrapy.Request(url, meta={"item": item}, callback=self.parse_detail)

# FormRequest
yield scrapy.FormRequest(url, formdata={"user": "admin"})

# JsonRequest
yield scrapy.http.JsonRequest(url, data={"key": "value"})
```

**Go:**
```go
// 基本请求
req, _ := shttp.NewRequest(url,
    shttp.WithCallback(s.ParseDetail),
)

// 带 meta
req, _ := shttp.NewRequest(url,
    shttp.WithCallback(s.ParseDetail),
    shttp.WithMeta(map[string]any{"item": item}),
)

// FormRequest
req, _ := shttp.NewFormRequest(url, map[string]string{"user": "admin"})

// JsonRequest
req, _ := shttp.NewJSONRequest(url, map[string]any{"key": "value"})
```

### Request 选项对照

| Python 参数 | Go Option | 说明 |
|-------------|-----------|------|
| `callback=` | `shttp.WithCallback(fn)` | 回调函数 |
| `errback=` | `shttp.WithErrback(fn)` | 错误回调 |
| `meta={}` | `shttp.WithMeta(map)` | 元数据 |
| `headers={}` | `shttp.WithHeaders(map)` | 请求头 |
| `dont_filter=True` | `shttp.WithDontFilter(true)` | 跳过去重 |
| `priority=1` | `shttp.WithPriority(1)` | 优先级 |
| `method="POST"` | `shttp.WithMethod("POST")` | HTTP 方法 |
| `body=b"..."` | `shttp.WithBody([]byte)` | 请求体 |
| `cookies={}` | `shttp.WithCookies(map)` | Cookie |

### Response API 对照

| Python | Go | 说明 |
|--------|-----|------|
| `response.url` | `response.URL` | `*url.URL` 类型 |
| `response.status` | `response.Status` | `int` 类型 |
| `response.text` | `response.Text()` | 方法调用 |
| `response.body` | `response.Body` | `[]byte` 类型 |
| `response.headers` | `response.Headers` | `Headers` 类型 |
| `response.css(...)` | `response.CSS(...)` | CSS 选择器 |
| `response.xpath(...)` | `response.XPath(...)` | XPath 选择器 |
| `response.urljoin(url)` | `response.URLJoin(url)` | URL 拼接 |
| `response.follow(url)` | `shttp.NewRequest(response.URLJoin(url))` | 需显式构造 |

---

## 选择器

### CSS 选择器对照

| Python | Go | 说明 |
|--------|-----|------|
| `response.css("div.quote")` | `response.CSS("div.quote")` | 返回选择器列表 |
| `.css("span::text").get()` | `.CSS("span::text").Get("")` | 获取第一个，默认值为参数 |
| `.css("span::text").getall()` | `.CSS("span::text").GetAll()` | 获取所有 |
| `.css("a::attr(href)").get()` | `.CSS("a::attr(href)").Get("")` | 获取属性 |
| `.css("div").css("span")` | `.CSS("div")[0].CSS("span")` | 链式选择 |

### XPath 选择器对照

| Python | Go | 说明 |
|--------|-----|------|
| `response.xpath("//div")` | `response.XPath("//div")` | XPath 选择 |
| `.xpath(".//span/text()").get()` | `.XPath("./span/text()").Get("")` | 相对路径 |
| `.xpath("@href").get()` | `.XPath("@href").Get("")` | 获取属性 |

### 关键差异

1. **Get() 需要默认值参数** — Go 版本 `Get("")` 需要传入默认值（Python 的 `get()` 默认返回 `None`）
2. **列表索引** — Go 使用 `[0]` 索引访问，Python 可以直接链式调用
3. **无 re() 方法** — Go 版本不提供内置正则提取，使用标准库 `regexp`

---

## Item 定义

### Python

```python
# 方式 1：scrapy.Item
class ProductItem(scrapy.Item):
    name = scrapy.Field()
    price = scrapy.Field()
    category = scrapy.Field()

# 方式 2：dict
yield {"name": "Product", "price": 29.99}

# 方式 3：dataclass
@dataclass
class Product:
    name: str
    price: float
```

### Go

```go
// 方式 1：struct + item tag（推荐）
type Product struct {
    Name     string  `item:"name"`
    Price    float64 `item:"price"`
    Category string  `item:"category"`
}

// 方式 2：map
item := map[string]any{
    "name":  "Product",
    "price": 29.99,
}
```

### ItemAdapter 对照

| Python (itemadapter) | Go (pkg/item) | 说明 |
|---------------------|---------------|------|
| `ItemAdapter(item)` | `item.Adapt(item)` | 创建适配器 |
| `adapter["field"]` | `adapter.Get("field")` | 获取字段 |
| `adapter["field"] = val` | `adapter.Set("field", val)` | 设置字段 |
| `adapter.field_names()` | `adapter.FieldNames()` | 字段列表 |
| `adapter.asdict()` | `adapter.AsMap()` | 转为 map |

---

## Item Pipeline

### Python

```python
class JsonWriterPipeline:
    def open_spider(self, spider):
        self.file = open("items.json", "w")
    
    def close_spider(self, spider):
        self.file.close()
    
    def process_item(self, item, spider):
        line = json.dumps(dict(item)) + "\n"
        self.file.write(line)
        return item

# settings.py
ITEM_PIPELINES = {
    "myproject.pipelines.JsonWriterPipeline": 300,
}
```

### Go

```go
type JsonWriterPipeline struct {
    file *os.File
}

func (p *JsonWriterPipeline) Open(ctx context.Context) error {
    f, err := os.Create("items.json")
    if err != nil {
        return err
    }
    p.file = f
    return nil
}

func (p *JsonWriterPipeline) Close(ctx context.Context) error {
    return p.file.Close()
}

func (p *JsonWriterPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
    data, _ := json.Marshal(item)
    _, err := p.file.Write(append(data, '\n'))
    return item, err
}

// 注册
c := crawler.NewDefault()
c.AddPipeline(&JsonWriterPipeline{}, "JsonWriter", 300)
```

### 迁移要点

| Python | Go | 说明 |
|--------|-----|------|
| `open_spider(self, spider)` | `Open(ctx context.Context) error` | 返回 error |
| `close_spider(self, spider)` | `Close(ctx context.Context) error` | 返回 error |
| `process_item(self, item, spider)` | `ProcessItem(ctx, item) (any, error)` | 返回处理后的 item 和 error |
| `raise DropItem("reason")` | `return nil, errors.ErrDropItem` | 丢弃 Item |
| `ITEM_PIPELINES = {...}` | `c.AddPipeline(p, name, priority)` | 代码注册 |

---

## 中间件迁移

### 下载器中间件

**Python:**
```python
class MyMiddleware:
    def process_request(self, request, spider):
        request.headers["X-Custom"] = "value"
        return None  # 继续处理

    def process_response(self, request, response, spider):
        return response  # 继续处理

    def process_exception(self, request, exception, spider):
        return None  # 继续处理

# settings.py
DOWNLOADER_MIDDLEWARES = {
    "myproject.middlewares.MyMiddleware": 543,
}
```

**Go:**
```go
type MyMiddleware struct{}

func (m *MyMiddleware) ProcessRequest(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
    req.Headers.Set("X-Custom", "value")
    return nil, nil // nil response = 继续处理链
}

func (m *MyMiddleware) ProcessResponse(ctx context.Context, req *shttp.Request, resp *shttp.Response) (*shttp.Response, error) {
    return resp, nil // 返回 response = 继续处理链
}

func (m *MyMiddleware) ProcessException(ctx context.Context, req *shttp.Request, err error) (*shttp.Response, error) {
    return nil, err // 返回 error = 继续处理链
}

// 注册
c := crawler.NewDefault()
c.AddDownloaderMiddleware(&MyMiddleware{}, "MyMiddleware", 543)
```

### 中间件返回值语义对照

| 场景 | Python | Go |
|------|--------|-----|
| 继续处理链 | `return None` | `return nil, nil` |
| 短路返回 Response | `return Response(...)` | `return resp, nil` |
| 丢弃请求 | `raise IgnoreRequest()` | `return nil, errors.ErrIgnoreRequest` |
| 产生新请求 | `return Request(...)` | `return nil, errors.NewRequestError(req)` |

---

## 配置迁移

### Python settings.py → Go 配置

| Python (settings.py) | Go (spider.Settings) | Go (TOML) |
|---------------------|---------------------|-----------|
| `CONCURRENT_REQUESTS = 16` | `ConcurrentRequests: spider.IntPtr(16)` | `concurrent_requests = 16` |
| `DOWNLOAD_DELAY = 1` | `DownloadDelay: spider.DurationPtr(time.Second)` | `download_delay = "1s"` |
| `DOWNLOAD_TIMEOUT = 180` | `DownloadTimeout: spider.DurationPtr(180*time.Second)` | `download_timeout = "180s"` |
| `RETRY_TIMES = 2` | `RetryTimes: spider.IntPtr(2)` | `retry_times = 2` |
| `RETRY_HTTP_CODES = [500, 502]` | `RetryHTTPCodes: []int{500, 502}` | `http_codes = [500, 502]` |
| `USER_AGENT = "..."` | `UserAgent: spider.StringPtr("...")` | `user_agent = "..."` |
| `LOG_LEVEL = "INFO"` | `LogLevel: spider.StringPtr("INFO")` | `log_level = "INFO"` |
| `DEPTH_LIMIT = 3` | `DepthLimit: spider.IntPtr(3)` | — |
| `ROBOTSTXT_OBEY = True` | Extra: `"ROBOTSTXT_OBEY": true` | — |
| `COOKIES_ENABLED = True` | — (默认启用) | — |
| `REDIRECT_MAX_TIMES = 20` | `RedirectMaxTimes: spider.IntPtr(20)` | — |

### 配置优先级对照

| Python 优先级 | Go 优先级 | 说明 |
|--------------|-----------|------|
| 默认值 (scrapy/settings/default_settings.py) | Default | 框架内置 |
| 命令行 (scrapy crawl -s KEY=VAL) | Cmdline | 最高优先级 |
| Spider.custom_settings | Spider | Spider 级别 |
| settings.py | Project | 项目级别 |

---

## Feed Export

### Python

```python
# settings.py
FEEDS = {
    "output.json": {
        "format": "json",
        "overwrite": True,
    },
    "output.csv": {
        "format": "csv",
        "fields": ["name", "price"],
    },
}
```

### Go

```go
c := crawler.NewDefault()

c.AddFeed(feedexport.FeedConfig{
    URI:       "file:///output.json",
    Format:    feedexport.FormatJSON,
    Overwrite: true,
})

c.AddFeed(feedexport.FeedConfig{
    URI:    "file:///output.csv",
    Format: feedexport.FormatCSV,
    Options: feedexport.ExporterOptions{
        FieldsToExport: []string{"name", "price"},
    },
})
```

### 格式对照

| Python 格式 | Go 格式常量 |
|------------|------------|
| `"json"` | `feedexport.FormatJSON` |
| `"jsonlines"` / `"jl"` | `feedexport.FormatJSONLines` |
| `"csv"` | `feedexport.FormatCSV` |
| `"xml"` | `feedexport.FormatXML` |

---

## 信号系统

### Python

```python
from scrapy import signals

class MyExtension:
    @classmethod
    def from_crawler(cls, crawler):
        ext = cls()
        crawler.signals.connect(ext.spider_opened, signal=signals.spider_opened)
        return ext
    
    def spider_opened(self, spider):
        print(f"Spider opened: {spider.name}")
```

### Go

```go
type MyExtension struct {
    signals *signal.Manager
}

func (e *MyExtension) Open(ctx context.Context) error {
    e.signals.Connect(signal.SpiderOpened, e.onSpiderOpened)
    return nil
}

func (e *MyExtension) Close(ctx context.Context) error {
    e.signals.Disconnect(signal.SpiderOpened, e.onSpiderOpened)
    return nil
}

func (e *MyExtension) onSpiderOpened(params map[string]any) error {
    sp := params["spider"].(spider.Spider)
    fmt.Printf("Spider opened: %s\n", sp.Name())
    return nil
}
```

### 信号名称对照

| Python | Go |
|--------|-----|
| `signals.engine_started` | `signal.EngineStarted` |
| `signals.engine_stopped` | `signal.EngineStopped` |
| `signals.spider_opened` | `signal.SpiderOpened` |
| `signals.spider_idle` | `signal.SpiderIdle` |
| `signals.spider_closed` | `signal.SpiderClosed` |
| `signals.request_scheduled` | `signal.RequestScheduled` |
| `signals.request_dropped` | `signal.RequestDropped` |
| `signals.response_received` | `signal.ResponseReceived` |
| `signals.item_scraped` | `signal.ItemScraped` |
| `signals.item_dropped` | `signal.ItemDropped` |
| `signals.item_error` | `signal.ItemError` |

---

## CrawlSpider 与 Rule

### Python

```python
from scrapy.spiders import CrawlSpider, Rule
from scrapy.linkextractors import LinkExtractor

class MySpider(CrawlSpider):
    name = "myspider"
    start_urls = ["https://example.com/"]
    
    rules = (
        Rule(LinkExtractor(allow=r"/category/"), follow=True),
        Rule(LinkExtractor(allow=r"/article/\d+"), callback="parse_article"),
    )
    
    def parse_article(self, response):
        yield {"title": response.css("h1::text").get()}
```

### Go

```go
type MySpider struct {
    spider.CrawlSpider
}

func NewMySpider() *MySpider {
    s := &MySpider{}
    s.SpiderName = "myspider"
    s.StartURLs = []string{"https://example.com/"}
    
    s.Rules = []spider.Rule{
        {
            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
                linkextractor.WithAllow(`/category/`),
            ),
            // Follow 默认 true（当 Callback 为 nil 时）
        },
        {
            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
                linkextractor.WithAllow(`/article/\d+`),
            ),
            Callback: s.parseArticle,
        },
    }
    
    return s
}

func (s *MySpider) parseArticle(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    item := map[string]any{"title": resp.CSS("h1::text").Get("")}
    return []spider.Output{{Item: item}}, nil
}
```

### Rule 参数对照

| Python | Go | 说明 |
|--------|-----|------|
| `LinkExtractor(allow=r"...")` | `linkextractor.NewHTMLLinkExtractor(WithAllow(...))` | 正则允许 |
| `LinkExtractor(deny=r"...")` | `linkextractor.WithDeny(...)` | 正则拒绝 |
| `LinkExtractor(allow_domains=[...])` | `linkextractor.WithAllowDomains(...)` | 允许域名 |
| `LinkExtractor(deny_domains=[...])` | `linkextractor.WithDenyDomains(...)` | 拒绝域名 |
| `LinkExtractor(restrict_css="...")` | `linkextractor.WithRestrictCSS(...)` | CSS 限制区域 |
| `callback="method_name"` | `Callback: s.methodName` | 直接引用方法 |
| `follow=True` | `Follow: true` | 是否跟踪 |
| `process_links=func` | `ProcessLinks: func` | 链接后处理 |
| `process_request=func` | `ProcessRequest: func` | 请求后处理 |

---

## 命令行工具

| Python Scrapy | scrapy-go | 说明 |
|---------------|-----------|------|
| `scrapy startproject name` | `scrapy-go startproject name` | 创建项目 |
| `scrapy genspider name domain` | `scrapy-go genspider name domain` | 生成 Spider |
| `scrapy crawl spidername` | `go run main.go` | 运行爬虫 |
| `scrapy shell url` | ❌ 不支持 | Go 编译型语言 |
| `scrapy view url` | ❌ 不支持 | — |
| `scrapy fetch url` | ❌ 不支持 | 使用 curl |
| `scrapy version` | `scrapy-go version` | 版本信息 |

---

## 不支持的特性

以下 Python Scrapy 特性在 scrapy-go 中**不提供**，以及推荐的替代方案：

| Scrapy 特性 | 原因 | 替代方案 |
|------------|------|---------|
| `scrapy shell` | Go 编译型语言，无 REPL | 编写测试代码 + `go test` |
| `Telnet Console` | 安全风险 | pprof HTTP 端点 |
| `Media Pipeline` | 复杂度高，使用场景有限 | 自定义 Pipeline 实现 |
| `AutoThrottle` | 计划在 Post-v1.0 实现 | 手动配置 `DOWNLOAD_DELAY` |
| `scrapy.contracts` | Go 有标准测试框架 | `go test` + 表驱动测试 |
| `Item Loader` | Go 类型系统已提供足够约束 | struct + `item` tag |
| `Splash/Playwright` | 外部依赖 | 自定义 Downloader Handler |
| `scrapy-redis` | 计划在 Post-v1.0 实现 | — |
| `from_crawler()` | Go 无类方法 | 构造函数 + `Open(ctx)` |
| `pickle` 序列化 | 跨语言兼容 | JSON 序列化 |

---

## 常见迁移模式

### 模式 1：yield → append + return

**Python（生成器模式）：**
```python
def parse(self, response):
    for item in response.css("div.item"):
        yield {"name": item.css("::text").get()}
    yield scrapy.Request(next_url)
```

**Go（收集返回模式）：**
```go
func (s *Spider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    var outputs []spider.Output
    for _, item := range resp.CSS("div.item") {
        outputs = append(outputs, spider.Output{
            Item: map[string]any{"name": item.CSS("::text").Get("")},
        })
    }
    req, _ := shttp.NewRequest(nextURL)
    outputs = append(outputs, spider.Output{Request: req})
    return outputs, nil
}
```

### 模式 2：meta 传递数据

**Python：**
```python
def parse(self, response):
    for url in response.css("a::attr(href)").getall():
        yield scrapy.Request(url, meta={"page": 1}, callback=self.parse_detail)

def parse_detail(self, response):
    page = response.meta["page"]
```

**Go：**
```go
func (s *Spider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    var outputs []spider.Output
    for _, url := range resp.CSS("a::attr(href)").GetAll() {
        req, _ := shttp.NewRequest(resp.URLJoin(url),
            shttp.WithCallback(s.ParseDetail),
            shttp.WithMeta(map[string]any{"page": 1}),
        )
        outputs = append(outputs, spider.Output{Request: req})
    }
    return outputs, nil
}

func (s *Spider) ParseDetail(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    page := resp.Request.Meta["page"].(int)
    // ...
}
```

### 模式 3：errback 错误处理

**Python：**
```python
def start_requests(self):
    yield scrapy.Request(url, callback=self.parse, errback=self.handle_error)

def handle_error(self, failure):
    self.logger.error(f"Request failed: {failure.value}")
```

**Go：**
```go
func (s *Spider) Start(ctx context.Context) <-chan spider.Output {
    ch := make(chan spider.Output)
    go func() {
        defer close(ch)
        req, _ := shttp.NewRequest(url,
            shttp.WithCallback(s.Parse),
            shttp.WithErrback(s.HandleError),
        )
        ch <- spider.Output{Request: req}
    }()
    return ch
}

func (s *Spider) HandleError(ctx context.Context, err error, req *shttp.Request) ([]spider.Output, error) {
    s.Logger.Error("Request failed", "error", err, "url", req.URL.String())
    return nil, nil
}
```

### 模式 4：from_crawler → 构造函数 + Open

**Python：**
```python
class MyPipeline:
    @classmethod
    def from_crawler(cls, crawler):
        return cls(
            mongo_uri=crawler.settings.get("MONGO_URI"),
        )
    
    def __init__(self, mongo_uri):
        self.mongo_uri = mongo_uri
```

**Go：**
```go
type MyPipeline struct {
    mongoURI string
    client   *mongo.Client
}

func NewMyPipeline(mongoURI string) *MyPipeline {
    return &MyPipeline{mongoURI: mongoURI}
}

func (p *MyPipeline) Open(ctx context.Context) error {
    client, err := mongo.Connect(ctx, options.Client().ApplyURI(p.mongoURI))
    if err != nil {
        return err
    }
    p.client = client
    return nil
}
```

---

## 性能对比

| 指标 | Python Scrapy | scrapy-go | 提升 |
|------|--------------|-----------|------|
| QPS（16 并发） | ~3,000 req/s | ~17,000 req/s | **5.7x** |
| 内存（10 万请求） | ~500 MB | ~139 MB | **3.6x** |
| 启动时间 | ~2s | ~10ms | **200x** |
| CPU 利用率 | 单核（GIL） | 多核 | 线性扩展 |
| 二进制大小 | N/A（解释型） | ~15 MB | 单文件部署 |

> 注：以上数据基于本地 benchmark 服务器测试，实际性能取决于网络延迟和目标网站响应速度。
> 在有网络延迟的真实场景中，两者差距会缩小（I/O bound 场景）。

### 适用场景建议

| 场景 | 推荐 | 原因 |
|------|------|------|
| 快速原型 | Python Scrapy | 动态类型 + shell 交互 |
| 高性能生产 | scrapy-go | 多核并行 + 低内存 |
| 大规模爬取 | scrapy-go | 更好的并发扩展性 |
| 团队协作 | scrapy-go | 编译期类型检查 |
| 单文件部署 | scrapy-go | 静态编译二进制 |
| 已有 Python 生态 | Python Scrapy | 丰富的第三方扩展 |

---

## 迁移检查清单

- [ ] 将 `scrapy.Spider` 子类改为嵌入 `spider.Base` 的结构体
- [ ] 将 `yield` 语句改为收集 `[]spider.Output` 后统一返回
- [ ] 将 `response.css()/xpath()` 改为大写方法名 `CSS()/XPath()`
- [ ] 将 `.get()` 改为 `.Get("")`（需要默认值参数）
- [ ] 将 `custom_settings` 字典改为 `CustomSettings()` 方法
- [ ] 将 Pipeline 类改为实现 `pipeline.ItemPipeline` 接口
- [ ] 将中间件类改为实现对应接口
- [ ] 将 `settings.py` 配置迁移到 `scrapy-go.toml` 或代码配置
- [ ] 将 `ITEM_PIPELINES` / `DOWNLOADER_MIDDLEWARES` 改为 `AddPipeline()` / `AddDownloaderMiddleware()`
- [ ] 将 `FEEDS` 配置改为 `AddFeed()` 调用
- [ ] 添加 `main()` 函数作为程序入口
- [ ] 运行 `go build` 确认编译通过
- [ ] 运行 `go test -race` 确认无竞态条件
