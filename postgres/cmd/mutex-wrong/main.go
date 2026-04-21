// Demonstrates why sync.Mutex does NOT fix a distributed race condition.
//
// In production, "Server A" and "Server B" are separate processes or pods.
// Each has its own mutex living in its own memory. They cannot see each other's lock.
// We simulate this by giving each simulated server its own independent mutex.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RaikaSurendra/double-spending/postgres/internal/db"
)

const (
	initialBalance = 2000
	perServer      = 50
	deductAmount   = 10
)

func main() {
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	if err := db.Seed(ctx, pool, initialBalance); err != nil {
		panic(err)
	}

	totalGoroutines := perServer * 2
	expected := initialBalance - (totalGoroutines * deductAmount)
	fmt.Printf("Initial balance : %d\n", initialBalance)
	fmt.Printf("Servers         : 2 (each with %d goroutines, each deducts %d)\n", perServer, deductAmount)
	fmt.Printf("Expected final  : %d\n\n", expected)
	fmt.Println("NOTE: Each server has its own sync.Mutex — they cannot see each other's lock.")

	// Two separate mutexes — one per "server process".
	var mutexA, mutexB sync.Mutex
	var wg sync.WaitGroup

	spawnServer := func(mu *sync.Mutex, n int) {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Locks THIS server's mutex only.
				// The other server has a DIFFERENT mutex in a DIFFERENT process.
				mu.Lock()
				defer mu.Unlock()

				var balance int
				pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&balance)
				time.Sleep(time.Millisecond) // widen race window
				pool.Exec(ctx, "UPDATE accounts SET balance = $1 WHERE id = 1", balance-deductAmount)
			}()
		}
	}

	spawnServer(&mutexA, perServer) // "Server A"
	spawnServer(&mutexB, perServer) // "Server B"
	wg.Wait()

	var final int
	pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&final)
	lost := final - expected

	fmt.Printf("\nActual final    : %d\n", final)
	if lost > 0 {
		fmt.Printf("DOUBLE-SPENT    : %d units — Mutex did not protect distributed state\n", lost)
	} else {
		fmt.Println("No race observed this run (non-deterministic — try again)")
	}
}
