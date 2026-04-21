// Optimistic locking: assume conflicts are rare; detect them instead of preventing them.
//
// Each row carries a version counter. The update only succeeds if the version
// in the database still matches what was read. If another transaction committed
// first and incremented the version, this update hits 0 rows — the caller retries
// with a fresh read.
//
// Use when:
//   - Contention is LOW (most reads proceed without conflict)
//   - You want to avoid the cost of holding locks while doing business logic
//   - You are OK with retry logic in the application
//
// Trade-off: under high contention, many retries make this SLOWER than SELECT FOR UPDATE.
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/RaikaSurendra/double-spending/postgres/internal/db"
)

const (
	initialBalance = 500
	goroutines     = 100
	deductAmount   = 10
	maxRetries     = 20
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

	fmt.Printf("Initial balance    : %d\n", initialBalance)
	fmt.Printf("Goroutines         : %d (each tries to deduct %d)\n", goroutines, deductAmount)
	fmt.Printf("Total attempted    : %d  (exceeds available funds)\n", goroutines*deductAmount)
	fmt.Printf("Expected final     : 0  (no overdraft)\n\n")

	var (
		wg           sync.WaitGroup
		succeeded    atomic.Int64
		insufficient atomic.Int64
		retried      atomic.Int64
		exhausted    atomic.Int64 // gave up after maxRetries — real limitation under high contention
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					retried.Add(1)
				}

				// Step 1: Read balance AND version together.
				var balance, version int
				err := pool.QueryRow(ctx,
					"SELECT balance, version FROM accounts WHERE id = 1",
				).Scan(&balance, &version)
				if err != nil {
					return
				}

				if balance < deductAmount {
					insufficient.Add(1)
					return
				}

				// Step 2: Write only if version hasn't changed since our read.
				// If another goroutine committed between Step 1 and here, this
				// hits 0 rows → retry with a fresh read.
				tag, err := pool.Exec(ctx, `
					UPDATE accounts
					   SET balance = $1,
					       version = $2
					 WHERE id      = 1
					   AND version = $3
				`, balance-deductAmount, version+1, version)
				if err != nil {
					return
				}

				if tag.RowsAffected() == 1 {
					// Our version matched — update committed successfully.
					succeeded.Add(1)
					return
				}
				// Version mismatch — someone else committed first. Loop and retry.
			}
			// Fell out of retry loop without success or insufficient-funds.
			// Under high contention this is expected — a real system would back off and re-queue.
			exhausted.Add(1)
		}()
	}
	wg.Wait()

	var final int
	pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&final)

	fmt.Printf("Succeeded          : %d\n", succeeded.Load())
	fmt.Printf("Insufficient funds : %d\n", insufficient.Load())
	fmt.Printf("Exhausted retries  : %d  ← gave up after %d attempts (high contention)\n", exhausted.Load(), maxRetries)
	fmt.Printf("Total retries      : %d\n", retried.Load())
	fmt.Printf("Actual final       : %d\n", final)

	if final < 0 {
		fmt.Printf("OVERDRAFT — balance went negative (%d)\n", final)
	} else if exhausted.Load() > 0 {
		fmt.Printf("NO OVERDRAFT — but %d goroutines gave up under contention.\n", exhausted.Load())
		fmt.Println("Trade-off: optimistic locking degrades under high write contention.")
		fmt.Println("Under low contention it outperforms SELECT FOR UPDATE.")
	} else {
		fmt.Println("CORRECT — optimistic locking prevented overdraft and double-spending")
	}
}
