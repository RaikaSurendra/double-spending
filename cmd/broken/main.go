// Demonstrates a distributed race condition: non-atomic GET → compute → SET
// Multiple goroutines (simulating multiple servers) read a stale value,
// compute locally, and overwrite each other — causing double-spending.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	key          = "balance:broken"
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

			// Step 1: Read current balance
			val, _ := rdb.Get(ctx, key).Int()

			// Step 2: Simulate a tiny bit of processing time.
			// This widens the race window — many goroutines read the same stale value.
			time.Sleep(time.Millisecond)

			// Step 3: Write back locally-computed value.
			// By now, other goroutines have already overwritten this key.
			rdb.Set(ctx, key, val-deductAmount, 0)
		}()
	}
	wg.Wait()

	final, _ := rdb.Get(ctx, key).Int()
	expected := initialFunds - (goroutines * deductAmount)
	lost := final - expected

	fmt.Printf("Actual final    : %d\n", final)
	if lost > 0 {
		fmt.Printf("DOUBLE-SPENT    : %d units lost due to race condition\n", lost)
	} else {
		fmt.Println("No race observed (try again — it's non-deterministic)")
	}
}
