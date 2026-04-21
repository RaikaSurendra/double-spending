// The simplest correct fix: push the arithmetic into the database.
//
// A single UPDATE statement that does "balance = balance - N" is evaluated
// atomically by Postgres. The row is locked for the duration of the statement;
// no other statement can read or write that row's balance during that time.
// There is no application-level read — no stale value, no race window.
//
// Add "AND balance >= $1" to prevent overdraft atomically in the same statement.
package main

import (
	"context"
	"fmt"
	"sync"

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
			// Single atomic statement — Postgres handles read + compute + write internally.
			// "AND balance >= $1" adds overdraft protection in the same atomic operation.
			pool.Exec(ctx,
				"UPDATE accounts SET balance = balance - $1 WHERE id = 1 AND balance >= $1",
				deductAmount,
			)
		}()
	}
	wg.Wait()

	var final int
	pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&final)

	fmt.Printf("Actual final    : %d\n", final)
	if final == expected {
		fmt.Println("CORRECT — atomic UPDATE eliminated the race condition")
	} else {
		fmt.Printf("Unexpected result (diff: %d)\n", final-expected)
	}
}
