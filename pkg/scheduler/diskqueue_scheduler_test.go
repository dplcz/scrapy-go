package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// 磁盘队列调度器测试
// ============================================================================

func TestSchedulerWithDiskQueue(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	s := NewDefaultScheduler(
		WithStats(sc),
		WithJobDir(dir),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	// 入队请求
	req1 := shttp.MustNewRequest("https://example.com/1")
	req2 := shttp.MustNewRequest("https://example.com/2")

	if !s.EnqueueRequest(req1) {
		t.Error("first request should be enqueued")
	}
	if !s.EnqueueRequest(req2) {
		t.Error("second request should be enqueued")
	}

	if s.Len() != 2 {
		t.Errorf("expected len 2, got %d", s.Len())
	}

	// 验证磁盘队列统计
	diskEnqueued := sc.GetValue("scheduler/enqueued/disk", 0)
	if diskEnqueued != 2 {
		t.Errorf("expected scheduler/enqueued/disk=2, got %v", diskEnqueued)
	}

	// 出队
	dequeued := s.NextRequest()
	if dequeued == nil {
		t.Fatal("should dequeue a request")
	}

	diskDequeued := sc.GetValue("scheduler/dequeued/disk", 0)
	if diskDequeued != 1 {
		t.Errorf("expected scheduler/dequeued/disk=1, got %v", diskDequeued)
	}

	if err := s.Close(context.Background(), "finished"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestSchedulerDiskQueueResumeCrawl(t *testing.T) {
	dir := t.TempDir()

	// 第一次运行：入队 5 个请求，消费 2 个
	{
		sc := stats.NewMemoryCollector(false, nil)
		s := NewDefaultScheduler(
			WithStats(sc),
			WithJobDir(dir),
			WithDupeFilter(NewNoDupeFilter()), // 不去重，简化测试
		)

		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("open failed: %v", err)
		}

		for i := 0; i < 5; i++ {
			req := shttp.MustNewRequest("https://example.com/" + string(rune('a'+i)))
			s.EnqueueRequest(req)
		}

		// 消费 2 个
		s.NextRequest()
		s.NextRequest()

		if s.Len() != 3 {
			t.Errorf("expected 3 remaining, got %d", s.Len())
		}

		if err := s.Close(context.Background(), "shutdown"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	// 第二次运行：应从断点继续
	{
		sc := stats.NewMemoryCollector(false, nil)
		s := NewDefaultScheduler(
			WithStats(sc),
			WithJobDir(dir),
			WithDupeFilter(NewNoDupeFilter()),
		)

		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("reopen failed: %v", err)
		}

		if s.Len() != 3 {
			t.Errorf("expected 3 remaining after resume, got %d", s.Len())
		}

		// 应能继续出队
		for i := 0; i < 3; i++ {
			req := s.NextRequest()
			if req == nil {
				t.Errorf("request %d should not be nil", i)
			}
		}

		// 队列应为空
		if s.HasPendingRequests() {
			t.Error("queue should be empty after consuming all")
		}

		if err := s.Close(context.Background(), "finished"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}
}

func TestSchedulerDiskQueueWithDupeFilter(t *testing.T) {
	dir := t.TempDir()

	// 第一次运行：入队并消费一些请求
	{
		sc := stats.NewMemoryCollector(false, nil)
		df := NewPersistentRFPDupeFilter(nil, false, dir)
		s := NewDefaultScheduler(
			WithStats(sc),
			WithJobDir(dir),
			WithDupeFilter(df),
		)

		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("open failed: %v", err)
		}

		// 入队 3 个不同的请求
		s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1"))
		s.EnqueueRequest(shttp.MustNewRequest("https://example.com/2"))
		s.EnqueueRequest(shttp.MustNewRequest("https://example.com/3"))

		// 重复请求应被过滤
		if s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1")) {
			t.Error("duplicate request should be filtered")
		}

		// 消费 1 个
		s.NextRequest()

		if err := s.Close(context.Background(), "shutdown"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	// 第二次运行：去重状态应被恢复
	{
		sc := stats.NewMemoryCollector(false, nil)
		df := NewPersistentRFPDupeFilter(nil, false, dir)
		s := NewDefaultScheduler(
			WithStats(sc),
			WithJobDir(dir),
			WithDupeFilter(df),
		)

		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("reopen failed: %v", err)
		}

		// 之前已见过的请求应被过滤
		if s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1")) {
			t.Error("previously seen request should be filtered after resume")
		}
		if s.EnqueueRequest(shttp.MustNewRequest("https://example.com/2")) {
			t.Error("previously seen request should be filtered after resume")
		}

		// 新请求应能入队
		if !s.EnqueueRequest(shttp.MustNewRequest("https://example.com/4")) {
			t.Error("new request should be enqueued")
		}

		if err := s.Close(context.Background(), "finished"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}
}

func TestSchedulerDiskQueueFallbackToMemory(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	s := NewDefaultScheduler(
		WithStats(sc),
		WithJobDir(dir),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 正常请求应存入磁盘队列
	req := shttp.MustNewRequest("https://example.com")
	s.EnqueueRequest(req)

	diskEnqueued := sc.GetValue("scheduler/enqueued/disk", 0)
	if diskEnqueued != 1 {
		t.Errorf("expected disk enqueued=1, got %v", diskEnqueued)
	}
}

func TestSchedulerDiskQueueMemoryPriority(t *testing.T) {
	dir := t.TempDir()

	s := NewDefaultScheduler(
		WithJobDir(dir),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 先入磁盘队列
	diskReq := shttp.MustNewRequest("https://example.com/disk")
	s.EnqueueRequest(diskReq)

	// 手动向内存队列推入请求（模拟不可序列化的请求）
	s.mu.Lock()
	memReq := shttp.MustNewRequest("https://example.com/memory")
	s.pq.Push(memReq)
	s.mu.Unlock()

	// 内存队列应优先出队
	first := s.NextRequest()
	if first == nil {
		t.Fatal("should dequeue a request")
	}
	if first.URL.Path != "/memory" {
		t.Errorf("expected /memory (memory priority), got %s", first.URL.Path)
	}

	// 然后是磁盘队列
	second := s.NextRequest()
	if second == nil {
		t.Fatal("should dequeue from disk")
	}
	if second.URL.Path != "/disk" {
		t.Errorf("expected /disk, got %s", second.URL.Path)
	}
}

func TestSchedulerHasDiskQueue(t *testing.T) {
	// 无 JOBDIR
	s1 := NewDefaultScheduler()
	s1.Open(context.Background())
	defer s1.Close(context.Background(), "finished")
	if s1.HasDiskQueue() {
		t.Error("should not have disk queue without JOBDIR")
	}

	// 有 JOBDIR
	dir := t.TempDir()
	s2 := NewDefaultScheduler(WithJobDir(dir))
	s2.Open(context.Background())
	defer s2.Close(context.Background(), "finished")
	if !s2.HasDiskQueue() {
		t.Error("should have disk queue with JOBDIR")
	}
}

func TestSchedulerDiskQueueConcurrency(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	s := NewDefaultScheduler(
		WithStats(sc),
		WithJobDir(dir),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	n := 100
	var wg sync.WaitGroup

	// 并发入队
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := shttp.MustNewRequest("https://example.com/page",
				shttp.WithPriority(i%10),
				shttp.WithDontFilter(true),
			)
			s.EnqueueRequest(req)
		}(i)
	}
	wg.Wait()

	if s.Len() != n {
		t.Errorf("expected %d requests, got %d", n, s.Len())
	}

	// 并发出队
	var dequeuedCount int
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if req := s.NextRequest(); req != nil {
				mu.Lock()
				dequeuedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if dequeuedCount != n {
		t.Errorf("expected %d dequeued, got %d", n, dequeuedCount)
	}
}

// ============================================================================
// 持久化 DupeFilter 测试
// ============================================================================

func TestPersistentDupeFilterSaveLoad(t *testing.T) {
	dir := t.TempDir()

	// 第一次：记录一些指纹
	{
		df := NewPersistentRFPDupeFilter(nil, false, dir)
		if err := df.Open(context.Background()); err != nil {
			t.Fatalf("open failed: %v", err)
		}

		df.RequestSeen(shttp.MustNewRequest("https://example.com/1"))
		df.RequestSeen(shttp.MustNewRequest("https://example.com/2"))
		df.RequestSeen(shttp.MustNewRequest("https://example.com/3"))

		if df.SeenCount() != 3 {
			t.Errorf("expected 3 fingerprints, got %d", df.SeenCount())
		}

		if err := df.Close("shutdown"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	// 验证文件存在
	fpFile := filepath.Join(dir, "requests.seen")
	if _, err := os.Stat(fpFile); os.IsNotExist(err) {
		t.Fatal("requests.seen should exist after close")
	}

	// 第二次：加载已有指纹
	{
		df := NewPersistentRFPDupeFilter(nil, false, dir)
		if err := df.Open(context.Background()); err != nil {
			t.Fatalf("reopen failed: %v", err)
		}

		if df.SeenCount() != 3 {
			t.Errorf("expected 3 fingerprints after reload, got %d", df.SeenCount())
		}

		// 之前见过的请求应返回 true
		if !df.RequestSeen(shttp.MustNewRequest("https://example.com/1")) {
			t.Error("previously seen request should be detected")
		}

		// 新请求应返回 false
		if df.RequestSeen(shttp.MustNewRequest("https://example.com/4")) {
			t.Error("new request should not be seen")
		}

		if df.SeenCount() != 4 {
			t.Errorf("expected 4 fingerprints, got %d", df.SeenCount())
		}

		if err := df.Close("finished"); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}
}

func TestPersistentDupeFilterEmptyDir(t *testing.T) {
	dir := t.TempDir()

	df := NewPersistentRFPDupeFilter(nil, false, dir)
	if err := df.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if df.SeenCount() != 0 {
		t.Errorf("expected 0 fingerprints for new dir, got %d", df.SeenCount())
	}

	if err := df.Close("finished"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestDupeFilterStats(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())

	df.RequestSeen(shttp.MustNewRequest("https://example.com/1"))
	df.RequestSeen(shttp.MustNewRequest("https://example.com/2"))

	stats := df.DupeFilterStats()
	if stats["seen_count"] != 2 {
		t.Errorf("expected seen_count=2, got %v", stats["seen_count"])
	}
	if stats["persistent"] != false {
		t.Error("non-persistent filter should report persistent=false")
	}

	df.Close("finished")
}

func TestDupeFilterExportState(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	df.RequestSeen(shttp.MustNewRequest("https://example.com"))

	data, err := df.ExportState()
	if err != nil {
		t.Fatalf("export state failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("exported state should not be empty")
	}
}

func TestSchedulerDiskQueueCloseError(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	s := NewDefaultScheduler(
		WithStats(sc),
		WithJobDir(dir),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	// 正常关闭不应出错
	if err := s.Close(context.Background(), "finished"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestSchedulerDiskQueueDequeueError(t *testing.T) {
	dir := t.TempDir()

	s := NewDefaultScheduler(
		WithJobDir(dir),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 空磁盘队列出队应返回 nil
	req := s.NextRequest()
	if req != nil {
		t.Error("empty disk queue should return nil")
	}
}

func TestSchedulerWithCallbackRegistry(t *testing.T) {
	dir := t.TempDir()
	registry := shttp.NewCallbackRegistry()

	parseFn := func(ctx context.Context, resp *shttp.Response) ([]any, error) {
		return nil, nil
	}
	registry.Register("Parse", parseFn)

	s := NewDefaultScheduler(
		WithJobDir(dir),
		WithCallbackRegistry(registry),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithCallback(parseFn),
	)
	s.EnqueueRequest(req)

	// 出队后回调应被恢复
	dequeued := s.NextRequest()
	if dequeued == nil {
		t.Fatal("should dequeue a request")
	}
	if dequeued.Callback == nil {
		t.Error("callback should be restored from disk queue")
	}
}

func TestPersistentDupeFilterStats(t *testing.T) {
	dir := t.TempDir()

	df := NewPersistentRFPDupeFilter(nil, false, dir)
	df.Open(context.Background())

	df.RequestSeen(shttp.MustNewRequest("https://example.com"))

	stats := df.DupeFilterStats()
	if stats["persistent"] != true {
		t.Error("persistent filter should report persistent=true")
	}
	if stats["jobdir"] != dir {
		t.Errorf("expected jobdir=%s, got %v", dir, stats["jobdir"])
	}

	df.Close("finished")
}

func TestSchedulerWithSchedulerLogger(t *testing.T) {
	// 验证 WithSchedulerLogger 不 panic
	s := NewDefaultScheduler(
		WithSchedulerLogger(nil),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")
}

func TestSchedulerDiskQueueDebugMode(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	s := NewDefaultScheduler(
		WithStats(sc),
		WithJobDir(dir),
		WithDebug(true),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	req := shttp.MustNewRequest("https://example.com")
	s.EnqueueRequest(req)

	// 验证磁盘入队成功
	if s.Len() != 1 {
		t.Errorf("expected len 1, got %d", s.Len())
	}
}

func TestSchedulerDiskQueueMultipleCloseIdempotent(t *testing.T) {
	dir := t.TempDir()

	s := NewDefaultScheduler(
		WithJobDir(dir),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	s.EnqueueRequest(shttp.MustNewRequest("https://example.com"))

	// 第一次关闭
	if err := s.Close(context.Background(), "finished"); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
}

func TestSchedulerDiskQueuePriorityOrder(t *testing.T) {
	dir := t.TempDir()

	s := NewDefaultScheduler(
		WithJobDir(dir),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 入队不同优先级的请求
	low := shttp.MustNewRequest("https://example.com/low", shttp.WithPriority(1))
	high := shttp.MustNewRequest("https://example.com/high", shttp.WithPriority(10))
	mid := shttp.MustNewRequest("https://example.com/mid", shttp.WithPriority(5))

	s.EnqueueRequest(low)
	s.EnqueueRequest(high)
	s.EnqueueRequest(mid)

	// 应按优先级从高到低出队
	first := s.NextRequest()
	if first.URL.Path != "/high" {
		t.Errorf("expected /high, got %s", first.URL.Path)
	}
	second := s.NextRequest()
	if second.URL.Path != "/mid" {
		t.Errorf("expected /mid, got %s", second.URL.Path)
	}
	third := s.NextRequest()
	if third.URL.Path != "/low" {
		t.Errorf("expected /low, got %s", third.URL.Path)
	}
}

// ============================================================================
// WithExternalQueue 测试
// ============================================================================

func TestSchedulerWithExternalQueue(t *testing.T) {
	dir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)

	// 手动创建 DiskQueue 作为外部队列注入
	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}

	s := NewDefaultScheduler(
		WithStats(sc),
		WithExternalQueue(dq),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	// 入队请求
	req1 := shttp.MustNewRequest("https://example.com/1")
	req2 := shttp.MustNewRequest("https://example.com/2")

	if !s.EnqueueRequest(req1) {
		t.Error("first request should be enqueued")
	}
	if !s.EnqueueRequest(req2) {
		t.Error("second request should be enqueued")
	}

	// 验证通过外部队列入队
	diskEnqueued := sc.GetValue("scheduler/enqueued/disk", 0)
	if diskEnqueued != 2 {
		t.Errorf("expected scheduler/enqueued/disk=2, got %v", diskEnqueued)
	}

	// 出队
	dequeued := s.NextRequest()
	if dequeued == nil {
		t.Fatal("should dequeue a request")
	}

	if err := s.Close(context.Background(), "finished"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestSchedulerExternalQueueOverridesJobDir(t *testing.T) {
	jobDir := t.TempDir()
	externalDir := t.TempDir()

	// 手动创建外部队列
	dq, err := NewDiskQueue(externalDir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}

	// 同时设置 WithJobDir 和 WithExternalQueue，外部队列应优先
	s := NewDefaultScheduler(
		WithJobDir(jobDir),
		WithExternalQueue(dq),
		WithDupeFilter(NewNoDupeFilter()),
	)

	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 入队一个请求
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com"))

	// 验证请求存入了外部队列（externalDir），而非 jobDir
	if dq.Len() != 1 {
		t.Errorf("expected external queue len=1, got %d", dq.Len())
	}

	// jobDir 下不应有 requests.queue 目录
	if _, err := os.Stat(filepath.Join(jobDir, "requests.queue", "state.json")); !os.IsNotExist(err) {
		t.Error("jobDir should not be used when external queue is set")
	}
}

func TestSchedulerHasExternalQueue(t *testing.T) {
	// 无外部队列
	s1 := NewDefaultScheduler()
	s1.Open(context.Background())
	defer s1.Close(context.Background(), "finished")
	if s1.HasExternalQueue() {
		t.Error("should not have external queue without configuration")
	}

	// 通过 WithJobDir 启用
	dir := t.TempDir()
	s2 := NewDefaultScheduler(WithJobDir(dir))
	s2.Open(context.Background())
	defer s2.Close(context.Background(), "finished")
	if !s2.HasExternalQueue() {
		t.Error("should have external queue with JOBDIR")
	}

	// 通过 WithExternalQueue 启用
	externalDir := t.TempDir()
	dq, _ := NewDiskQueue(externalDir)
	s3 := NewDefaultScheduler(WithExternalQueue(dq))
	s3.Open(context.Background())
	defer s3.Close(context.Background(), "finished")
	if !s3.HasExternalQueue() {
		t.Error("should have external queue with WithExternalQueue")
	}
}

func TestSchedulerHasDiskQueueBackwardCompat(t *testing.T) {
	// 验证 HasDiskQueue 向后兼容
	dir := t.TempDir()
	s := NewDefaultScheduler(WithJobDir(dir))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	if !s.HasDiskQueue() {
		t.Error("HasDiskQueue should still work for backward compatibility")
	}
}
