package redisqueue

import "time"

// Options 定义 Redis 队列和去重过滤器的配置选项。
//
// 通过 DefaultOptions() 获取默认配置，然后按需修改。
type Options struct {
	// Addr 是 Redis 服务器地址，格式为 "host:port"。
	// 默认值："localhost:6379"
	Addr string

	// Password 是 Redis 认证密码。
	// 默认值：""（无密码）
	Password string

	// DB 是 Redis 数据库编号。
	// 默认值：0
	DB int

	// KeyPrefix 是所有 Redis Key 的前缀。
	// 用于隔离不同爬虫或不同环境的数据。
	// 默认值："scrapy-go"
	//
	// 实际 Key 格式：
	//   - 队列：{KeyPrefix}:queue
	//   - 去重集合：{KeyPrefix}:dupefilter
	KeyPrefix string

	// QueueKey 是队列的 Redis Key 后缀。
	// 完整 Key 为 "{KeyPrefix}:{QueueKey}"。
	// 默认值："queue"
	QueueKey string

	// DupeFilterKey 是去重集合的 Redis Key 后缀。
	// 完整 Key 为 "{KeyPrefix}:{DupeFilterKey}"。
	// 默认值："dupefilter"
	DupeFilterKey string

	// DialTimeout 是连接 Redis 的超时时间。
	// 默认值：5s
	DialTimeout time.Duration

	// ReadTimeout 是 Redis 读操作超时时间。
	// 默认值：3s
	ReadTimeout time.Duration

	// WriteTimeout 是 Redis 写操作超时时间。
	// 默认值：3s
	WriteTimeout time.Duration

	// PoolSize 是 Redis 连接池大小。
	// 默认值：10
	PoolSize int

	// StartURLsKey 是起始 URL 列表的 Redis Key 后缀（可选）。
	// 完整 Key 为 "{KeyPrefix}:{StartURLsKey}"。
	// 当设置此项时，可从 Redis List 中读取起始 URL。
	// 默认值："start_urls"
	StartURLsKey string

	// FlushOnStart 控制是否在启动时清空队列和去重集合。
	// 设置为 true 时，每次启动都从头开始爬取。
	// 默认值：false
	FlushOnStart bool

	// Serializer 指定序列化格式。
	// 当前仅支持 "json"。
	// 默认值："json"
	Serializer string

	// BloomFilterEnabled 控制是否启用本地布隆过滤器作为一级去重缓存。
	//
	// 启用后，RedisDupeFilter 在调用 Redis SADD 之前先通过本地布隆过滤器
	// 快速判断请求是否为新请求：
	//   - 布隆过滤器判断"不存在" → 100% 是新请求，直接写入 Redis 并返回
	//   - 布隆过滤器判断"可能存在" → 穿透到 Redis 做精确判断
	//
	// 在新请求占绝大多数的爬虫场景中，可大幅减少 Redis 读查询量。
	// 默认值：false
	BloomFilterEnabled bool

	// BloomExpectedItems 是布隆过滤器预估的不重复请求总数。
	// 该值影响布隆过滤器的内存占用和误判率。
	// 默认值：1000000（100 万）
	BloomExpectedItems uint

	// BloomFalsePositiveRate 是布隆过滤器可接受的误判率。
	// 误判率越低，内存占用越大。
	// 典型值：0.01（1%）对应 ~1.14 MB/百万条目，0.001（0.1%）对应 ~1.71 MB/百万条目。
	// 默认值：0.001（0.1%）
	BloomFalsePositiveRate float64
}

// DefaultOptions 返回默认的 Redis 配置选项。
func DefaultOptions() *Options {
	return &Options{
		Addr:                   "localhost:6379",
		Password:               "",
		DB:                     0,
		KeyPrefix:              "scrapy-go",
		QueueKey:               "queue",
		DupeFilterKey:          "dupefilter",
		DialTimeout:            5 * time.Second,
		ReadTimeout:            3 * time.Second,
		WriteTimeout:           3 * time.Second,
		PoolSize:               10,
		StartURLsKey:           "start_urls",
		FlushOnStart:           false,
		Serializer:             "json",
		BloomFilterEnabled:     false,
		BloomExpectedItems:     1_000_000,
		BloomFalsePositiveRate: 0.001,
	}
}

// queueFullKey 返回队列的完整 Redis Key。
func (o *Options) queueFullKey() string {
	return o.KeyPrefix + ":" + o.QueueKey
}

// dupeFilterFullKey 返回去重集合的完整 Redis Key。
func (o *Options) dupeFilterFullKey() string {
	return o.KeyPrefix + ":" + o.DupeFilterKey
}

// startURLsFullKey 返回起始 URL 列表的完整 Redis Key。
func (o *Options) startURLsFullKey() string {
	return o.KeyPrefix + ":" + o.StartURLsKey
}
