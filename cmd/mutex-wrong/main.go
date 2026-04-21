// Demonstrates why a sync.Mutex does NOT fix a distributed race condition.
//
// Each "server" has its own in-process mutex. Server A's mutex is invisible
// to Server B — they run in separate processes with separate memory spaces.
// We simulate this by giving each simulated server its own independent mutex.
//
// Within a single server the mutex enforces order, but across servers the
// race window is wide open: both servers can read the same stale balance
// from Redis before either one writes back.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	key          = "balance:mutex"
	initialFunds = 2000
	perServer    = 50  // goroutines per simulated server
	deductAmount = 10
)

func runServer(ctx context.Context, rdb *redis.Client, name string, n int, mu *sync.Mutex, wg *sync.WaitGroup) {
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Each server locks its OWN mutex — but the other server has a different one.
			mu.Lock()
			defer mu.Unlock()

			val, _ := rdb.Get(ctx, key).Int()
			time.Sleep(time.Millisecond) // widen race window
			rdb.Set(ctx, key, val-deductAmount, 0)
		}()
	}
}

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()

	rdb.Set(ctx, key, initialFunds, 0)

	totalGoroutines := perServer * 2
	fmt.Printf("Initial balance : %d\n", initialFunds)
	fmt.Printf("Servers         : 2 (each with %d goroutines, each deducts %d)\n", perServer, deductAmount)
	fmt.Printf("Expected final  : %d\n\n", initialFunds-(totalGoroutines*deductAmount))
	fmt.Println("NOTE: Each server has its own sync.Mutex — they cannot see each other's lock.")

	var mutexA, mutexB sync.Mutex
	var wg sync.WaitGroup

	runServer(ctx, rdb, "Server-A", perServer, &mutexA, &wg)
	runServer(ctx, rdb, "Server-B", perServer, &mutexB, &wg)
	wg.Wait()

	final, _ := rdb.Get(ctx, key).Int()
	expected := initialFunds - (totalGoroutines * deductAmount)
	lost := final - expected

	fmt.Printf("\nActual final    : %d\n", final)
	if lost > 0 {
		fmt.Printf("DOUBLE-SPENT    : %d units — Mutex did not protect distributed state\n", lost)
	} else {
		fmt.Println("No race observed this run (it's non-deterministic — try again)")
	}
}
