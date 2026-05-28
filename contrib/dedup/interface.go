package dedup

import "github.com/dplcz/scrapy-go/pkg/scheduler"

var _ scheduler.DupeFilter = (*URLCanonicalDupeFilter)(nil)
var _ scheduler.DupeFilter = (*SimHashDupeFilter)(nil)
var _ scheduler.DupeFilter = (*CompositeDupeFilter)(nil)
