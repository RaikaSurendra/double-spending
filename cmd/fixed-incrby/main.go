// The correct fix for simple balance deductions: push the arithmetic into Redis.
//
// Redis executes commands on a single main thread.
// DECRBY is a single atomic instruction — there is no gap between read and write.
// No goroutine, no server, no race can interleave between them.
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

const (
	key          = "balance:incrby"
	initialFunds = 2000
	goroutines   = 100
	deductAmount = 10
)

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()

	rdb.Set(ctx, key, initialFunds, 0)
	fmt.Printf("Initial balance : %d\n", initialFunds)
	fmt.Printf("Goroutines      : %d (each deducts %d)\n", goroutines, deductAmount)
	fmt.Printf("Expected final  : %d\n\n", initialFunds-(goroutines*deductAmount))

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Single atomic instruction — no read-modify-write gap.
			rdb.DecrBy(ctx, key, deductAmount)
		}()
	}
	wg.Wait()

	final, _ := rdb.Get(ctx, key).Int()
	expected := initialFunds - (goroutines * deductAmount)

	fmt.Printf("Actual final    : %d\n", final)
	if final == expected {
		fmt.Println("CORRECT — atomic DECRBY eliminated the race condition")
	} else {
		fmt.Printf("Unexpected result (diff: %d)\n", final-expected)
	}
}
