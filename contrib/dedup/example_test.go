package dedup_test

import (
	"context"
	"fmt"

	"github.com/dplcz/scrapy-go/contrib/dedup"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

func ExampleNewDupeFilter() {
	filter, _ := dedup.NewDupeFilter(dedup.DefaultOptions())
	sched := scheduler.NewDefaultScheduler(scheduler.WithDupeFilter(filter))
	_ = sched.Open(context.Background())
	defer sched.Close(context.Background(), "finished")

	first := shttp.MustNewRequest("https://example.com/article?id=1&utm_source=news")
	second := shttp.MustNewRequest("https://example.com/article?fbclid=abc&id=1")

	fmt.Println(sched.EnqueueRequest(first))
	fmt.Println(sched.EnqueueRequest(second))

	// Output:
	// true
	// false
}
