package dedup

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

const (
	// MetaContentKey 是 SimHashDupeFilter 默认读取的请求 Meta 内容字段。
	MetaContentKey = "dedup_content"

	defaultHammingThreshold = 3
	maxHammingThreshold     = 15
	maxBandCount            = 16
)

// ContentExtractor 从请求中提取用于 SimHash 的内容。
// 返回 ok=false 表示当前请求没有可用于近似去重的内容，过滤器会跳过该策略。
type ContentExtractor func(request *shttp.Request) (content []byte, ok bool)

// SimHashOptions 定义 SimHash 近似去重配置。
type SimHashOptions struct {
	// HammingThreshold 是判定为近似重复的最大汉明距离。
	// 默认值：3。允许范围：0-15。
	HammingThreshold int

	// BandCount 是 LSH 分桶数量。为 0 时根据阈值自动选择。
	// 自动值为 max(4, HammingThreshold+1)，上限 16。
	BandCount int

	// MetaContentKey 是默认内容提取器读取的请求 Meta key。
	// 默认值：dedup_content。
	MetaContentKey string

	// ContentExtractor 是自定义内容提取器。
	// 为 nil 时使用 MetaContentKey 从 Request.Meta 中读取 string 或 []byte。
	ContentExtractor ContentExtractor
}

// DefaultSimHashOptions 返回 SimHash 默认配置。
func DefaultSimHashOptions() *SimHashOptions {
	return &SimHashOptions{
		HammingThreshold: defaultHammingThreshold,
		BandCount:        4,
		MetaContentKey:   MetaContentKey,
	}
}

// SimHash 计算文本内容的 64 位 SimHash 指纹。
func SimHash(text string) uint64 {
	return SimHashBytes([]byte(text))
}

// SimHashBytes 计算字节内容的 64 位 SimHash 指纹。
func SimHashBytes(content []byte) uint64 {
	tokens := tokenizeContent(string(content))
	if len(tokens) == 0 {
		return 0
	}

	weights := make(map[string]int, len(tokens))
	for _, token := range tokens {
		weights[token]++
	}

	var vector [64]int
	for token, weight := range weights {
		hash := hashToken(token)
		for bit := 0; bit < 64; bit++ {
			if hash&(uint64(1)<<bit) != 0 {
				vector[bit] += weight
			} else {
				vector[bit] -= weight
			}
		}
	}

	var fp uint64
	for bit, weight := range vector {
		if weight > 0 {
			fp |= uint64(1) << bit
		}
	}
	return fp
}

// HammingDistance 返回两个 64 位指纹之间的汉明距离。
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// SimHashDupeFilter 是基于内容 SimHash 的近似去重过滤器。
//
// 它实现 scheduler.DupeFilter 接口。为避免大规模线性扫描，过滤器使用
// LSH 分桶索引快速缩小候选集合；默认阈值为 3，分 4 个 band，可保证
// 汉明距离 <= 3 的重复内容至少命中一个相同 band。
type SimHashDupeFilter struct {
	opts      *SimHashOptions
	threshold int
	bandCount int
	bandSize  int
	extractor ContentExtractor

	mu        sync.Mutex
	seen      map[uint64]struct{}
	bandIndex map[uint64][]uint64
	seenCount atomic.Int64
	closed    atomic.Bool
}

// NewSimHashDupeFilter 创建 SimHash 近似去重过滤器。
func NewSimHashDupeFilter(opts *SimHashOptions) (*SimHashDupeFilter, error) {
	normalized, err := normalizeSimHashOptions(opts)
	if err != nil {
		return nil, err
	}

	bandSize := 64 / normalized.BandCount
	if bandSize == 0 {
		return nil, errors.New("dedup: SimHash BandCount is too large")
	}

	filter := &SimHashDupeFilter{
		opts:      normalized,
		threshold: normalized.HammingThreshold,
		bandCount: normalized.BandCount,
		bandSize:  bandSize,
		extractor: normalized.ContentExtractor,
		seen:      make(map[uint64]struct{}),
		bandIndex: make(map[uint64][]uint64),
	}
	return filter, nil
}

// Open 初始化过滤器。
func (f *SimHashDupeFilter) Open(ctx context.Context) error {
	f.closed.Store(false)
	return nil
}

// Close 关闭过滤器并释放内存状态。
func (f *SimHashDupeFilter) Close(reason string) error {
	if !f.closed.CompareAndSwap(false, true) {
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = make(map[uint64]struct{})
	f.bandIndex = make(map[uint64][]uint64)
	f.seenCount.Store(0)
	return nil
}

// RequestSeen 检查请求内容是否与已见内容近似重复。
//
// 如果请求中没有可提取内容，则返回 false，表示跳过该策略。
func (f *SimHashDupeFilter) RequestSeen(request *shttp.Request) bool {
	if request == nil || f.closed.Load() {
		return false
	}

	content, ok := f.extractor(request)
	if !ok || len(strings.TrimSpace(string(content))) == 0 {
		return false
	}

	fp := SimHashBytes(content)
	return f.requestSeenFingerprint(fp)
}

// SeenCount 返回已记录的 SimHash 指纹数量。
func (f *SimHashDupeFilter) SeenCount() int {
	return int(f.seenCount.Load())
}

// ContainsFingerprint 检查指定 SimHash 指纹是否与已有指纹近似重复。
func (f *SimHashDupeFilter) ContainsFingerprint(fp uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasNearDuplicateLocked(fp)
}

// AddFingerprint 直接添加一个 SimHash 指纹。
// 如果已有近似重复指纹，返回 true；否则记录并返回 false。
func (f *SimHashDupeFilter) AddFingerprint(fp uint64) bool {
	return f.requestSeenFingerprint(fp)
}

func (f *SimHashDupeFilter) requestSeenFingerprint(fp uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.hasNearDuplicateLocked(fp) {
		return true
	}
	f.addFingerprintLocked(fp)
	return false
}

func (f *SimHashDupeFilter) hasNearDuplicateLocked(fp uint64) bool {
	candidates := make(map[uint64]struct{})
	for band := 0; band < f.bandCount; band++ {
		key := f.bandKey(fp, band)
		for _, candidate := range f.bandIndex[key] {
			candidates[candidate] = struct{}{}
		}
	}

	for candidate := range candidates {
		if HammingDistance(fp, candidate) <= f.threshold {
			return true
		}
	}
	return false
}

func (f *SimHashDupeFilter) addFingerprintLocked(fp uint64) {
	if _, exists := f.seen[fp]; exists {
		return
	}
	f.seen[fp] = struct{}{}
	for band := 0; band < f.bandCount; band++ {
		key := f.bandKey(fp, band)
		f.bandIndex[key] = append(f.bandIndex[key], fp)
	}
	f.seenCount.Add(1)
}

func (f *SimHashDupeFilter) bandKey(fp uint64, band int) uint64 {
	start := band * f.bandSize
	width := f.bandSize
	if band == f.bandCount-1 {
		width = 64 - start
	}

	mask := ^uint64(0)
	if width < 64 {
		mask = (uint64(1) << uint(width)) - 1
	}
	value := (fp >> uint(start)) & mask
	return (uint64(band) << 56) | value
}

func normalizeSimHashOptions(opts *SimHashOptions) (*SimHashOptions, error) {
	if opts == nil {
		opts = DefaultSimHashOptions()
	}

	normalized := *opts
	if normalized.HammingThreshold < 0 || normalized.HammingThreshold > maxHammingThreshold {
		return nil, fmt.Errorf("dedup: SimHash HammingThreshold must be between 0 and %d", maxHammingThreshold)
	}
	if normalized.BandCount == 0 {
		normalized.BandCount = normalized.HammingThreshold + 1
		if normalized.BandCount < 4 {
			normalized.BandCount = 4
		}
		if normalized.BandCount > maxBandCount {
			normalized.BandCount = maxBandCount
		}
	}
	if normalized.BandCount < 1 || normalized.BandCount > maxBandCount {
		return nil, fmt.Errorf("dedup: SimHash BandCount must be between 1 and %d", maxBandCount)
	}
	if normalized.MetaContentKey == "" {
		normalized.MetaContentKey = MetaContentKey
	}
	if normalized.ContentExtractor == nil {
		key := normalized.MetaContentKey
		normalized.ContentExtractor = func(request *shttp.Request) ([]byte, bool) {
			value, ok := request.GetMeta(key)
			if !ok || value == nil {
				return nil, false
			}
			switch v := value.(type) {
			case string:
				return []byte(v), true
			case []byte:
				return v, true
			case fmt.Stringer:
				return []byte(v.String()), true
			default:
				return []byte(fmt.Sprint(v)), true
			}
		}
	}
	return &normalized, nil
}

func tokenizeContent(content string) []string {
	content = strings.ToLower(content)
	tokens := make([]string, 0, len(content)/6)
	var builder strings.Builder

	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String())
		builder.Reset()
	}

	for _, r := range content {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if isCJK(r) {
				flush()
				tokens = append(tokens, string(r))
			} else {
				builder.WriteRune(r)
			}
		default:
			flush()
		}
	}
	flush()
	return tokens
}

func isCJK(r rune) bool {
	return (r >= '\u4e00' && r <= '\u9fff') ||
		(r >= '\u3400' && r <= '\u4dbf') ||
		(r >= '\uf900' && r <= '\ufaff')
}

func hashToken(token string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(token))
	return h.Sum64()
}
