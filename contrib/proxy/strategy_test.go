package proxy

import (
	"testing"
)

func makeProxies(n int) []*Proxy {
	proxies := make([]*Proxy, n)
	for i := 0; i < n; i++ {
		p := &Proxy{
			URL:    "http://proxy.example.com:" + itoa(8080+i),
			Weight: 1,
		}
		proxies[i] = p
	}
	return proxies
}

// itoa 简易整数转字符串，避免引入 strconv 增加测试编译开销。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestRoundRobinStrategy(t *testing.T) {
	t.Parallel()

	s := NewRoundRobinStrategy()
	proxies := makeProxies(3)

	// 期望按 0, 1, 2, 0, 1, 2 ... 的顺序循环
	expected := []int{0, 1, 2, 0, 1, 2, 0}
	for i, want := range expected {
		got := s.Select(proxies)
		if got != proxies[want] {
			t.Errorf("step %d: want index %d (%s), got %s", i, want, proxies[want].URL, got.URL)
		}
	}
}

func TestRoundRobinStrategy_EmptyCandidates(t *testing.T) {
	t.Parallel()

	s := NewRoundRobinStrategy()
	if got := s.Select(nil); got != nil {
		t.Errorf("expected nil for empty candidates, got %v", got)
	}
	if got := s.Select([]*Proxy{}); got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

func TestRoundRobinStrategy_Concurrent(t *testing.T) {
	t.Parallel()

	s := NewRoundRobinStrategy()
	proxies := makeProxies(4)

	const goroutines = 50
	const iterations = 100

	counts := make(chan int, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			localHits := make(map[*Proxy]int)
			for i := 0; i < iterations; i++ {
				p := s.Select(proxies)
				localHits[p]++
			}
			counts <- len(localHits)
		}()
	}

	for g := 0; g < goroutines; g++ {
		hits := <-counts
		if hits == 0 {
			t.Errorf("expected non-zero hits in goroutine, got 0")
		}
	}
}

func TestRandomStrategy(t *testing.T) {
	t.Parallel()

	s := NewRandomStrategy()
	proxies := makeProxies(5)

	hits := make(map[*Proxy]int)
	for i := 0; i < 1000; i++ {
		p := s.Select(proxies)
		hits[p]++
	}

	// 5 个代理在 1000 次随机选择中应每个都被命中（绝大多数情况）
	if len(hits) < 5 {
		t.Errorf("expected all 5 proxies to be hit at least once, got %d", len(hits))
	}
}

func TestRandomStrategy_Empty(t *testing.T) {
	t.Parallel()

	s := NewRandomStrategy()
	if got := s.Select(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestWeightedStrategy_SmallPool(t *testing.T) {
	t.Parallel()

	s := NewWeightedStrategy()

	// 构造 3 个代理，权重比 1:2:7
	proxies := []*Proxy{
		{URL: "p1", Weight: 1},
		{URL: "p2", Weight: 2},
		{URL: "p3", Weight: 7},
	}

	const iterations = 10000
	hits := make(map[string]int)
	for i := 0; i < iterations; i++ {
		p := s.Select(proxies)
		hits[p.URL]++
	}

	// 期望比例约 10% / 20% / 70%，允许 5% 误差
	for url, expectedRatio := range map[string]float64{
		"p1": 0.1,
		"p2": 0.2,
		"p3": 0.7,
	} {
		actualRatio := float64(hits[url]) / float64(iterations)
		diff := actualRatio - expectedRatio
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.05 {
			t.Errorf("URL %s: expected ratio ~%.2f, got %.2f (diff=%.2f)",
				url, expectedRatio, actualRatio, diff)
		}
	}
}

func TestWeightedStrategy_LargePool(t *testing.T) {
	t.Parallel()

	s := NewWeightedStrategy()

	// 构造 16 个代理（触发二分查找路径），权重均为 1
	proxies := make([]*Proxy, 16)
	for i := range proxies {
		proxies[i] = &Proxy{URL: "p" + itoa(i), Weight: 1}
	}

	hits := make(map[string]int)
	for i := 0; i < 5000; i++ {
		p := s.Select(proxies)
		hits[p.URL]++
	}

	// 应当每个代理都被命中
	if len(hits) < 16 {
		t.Errorf("expected 16 proxies hit, got %d", len(hits))
	}
}

func TestWeightedStrategy_ZeroWeight(t *testing.T) {
	t.Parallel()

	s := NewWeightedStrategy()
	proxies := []*Proxy{
		{URL: "p1", Weight: 0},
		{URL: "p2", Weight: -1},
	}
	// 全部权重 <= 0 时按 1 处理，不应 panic
	for i := 0; i < 100; i++ {
		if got := s.Select(proxies); got == nil {
			t.Errorf("unexpected nil at iteration %d", i)
		}
	}
}

func TestWeightedStrategy_SingleProxy(t *testing.T) {
	t.Parallel()

	s := NewWeightedStrategy()
	proxies := []*Proxy{{URL: "only", Weight: 5}}
	for i := 0; i < 10; i++ {
		got := s.Select(proxies)
		if got != proxies[0] {
			t.Errorf("expected only proxy, got %v", got)
		}
	}
}

func TestNewStrategyByKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind StrategyKind
		want string
	}{
		{StrategyRoundRobin, "round_robin"},
		{StrategyRandom, "random"},
		{StrategyWeighted, "weighted"},
		{"unknown_kind", "round_robin"}, // 默认回退到 RoundRobin
	}

	for _, c := range cases {
		s := newStrategyByKind(c.kind)
		if s.Name() != c.want {
			t.Errorf("kind %q: want strategy %q, got %q", c.kind, c.want, s.Name())
		}
	}
}
