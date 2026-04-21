// Demonstrates a distributed race condition: non-atomic SELECT + UPDATE.
//
// Each goroutine reads the balance, computes the new value in application memory,
// then writes it back. The gap between SELECT and UPDATE is a wide-open race window:
// any other goroutine can read the same stale value and overwrite the result.
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
	goroutines     = 100
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

	expected := initialBalance - (goroutines * deductAmount)
	fmt.Printf("Initial balance : %d\n", initialBalance)
	fmt.Printf("Goroutines      : %d (each deducts %d)\n", goroutines, deductAmount)
	fmt.Printf("Expected final  : %d\n\n", expected)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Step 1: Read current balance.
			var balance int
			pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&balance)

			// Step 2: Simulate processing time — widens the race window.
			// Many goroutines read the same stale value before any one writes back.
			time.Sleep(time.Millisecond)

			// Step 3: Write locally-computed value back.
			// By now other goroutines have already updated the row.
			pool.Exec(ctx, "UPDATE accounts SET balance = $1 WHERE id = 1", balance-deductAmount)
		}()
	}
	wg.Wait()

	var final int
	pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&final)
	lost := final - expected

	fmt.Printf("Actual final    : %d\n", final)
	if lost > 0 {
		fmt.Printf("DOUBLE-SPENT    : %d units lost due to race condition\n", lost)
	} else {
		fmt.Println("No race observed (non-deterministic — try again)")
	}
}
