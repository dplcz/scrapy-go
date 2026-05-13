module github.com/dplcz/scrapy-go/contrib/ratelimit

go 1.25.1

require (
	github.com/alicebob/miniredis/v2 v2.34.0
	github.com/dplcz/scrapy-go v1.1.0
	github.com/redis/go-redis/v9 v9.7.3
)

require (
	github.com/alicebob/gopher-json v0.0.0-20230218143504-906a9b012302 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
)

replace github.com/dplcz/scrapy-go => ../../
