// Pessimistic locking: SELECT ... FOR UPDATE acquires an exclusive row lock.
//
// Any other transaction that tries to SELECT FOR UPDATE (or UPDATE) the same row
// will block until this transaction commits or rolls back. This serialises all
// concurrent deductions at the database level — works across processes and servers.
//
// Use when:
//   - You need to read the value AND make a decision based on it before writing
//   - Contention is high and you need strict serialisation
//   - You cannot express the check as a single UPDATE statement
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/RaikaSurendra/double-spending/postgres/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	initialBalance = 500  // intentionally low: total attempted deduction (1000) exceeds it
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

	fmt.Printf("Initial balance    : %d\n", initialBalance)
	fmt.Printf("Goroutines         : %d (each tries to deduct %d)\n", goroutines, deductAmount)
	fmt.Printf("Total attempted    : %d  (exceeds available funds)\n", goroutines*deductAmount)
	fmt.Printf("Expected final     : 0  (no overdraft)\n\n")

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		succeeded    int
		insufficient int
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				return
			}
			defer tx.Rollback(ctx)

			// Lock the row. Any concurrent SELECT FOR UPDATE on id=1 will WAIT here.
			var balance int
			err = tx.QueryRow(ctx,
				"SELECT balance FROM accounts WHERE id = 1 FOR UPDATE",
			).Scan(&balance)
			if err != nil {
				return
			}

			if balance < deductAmount {
				mu.Lock()
				insufficient++
				mu.Unlock()
				return // rollback — nothing written
			}

			_, err = tx.Exec(ctx,
				"UPDATE accounts SET balance = $1 WHERE id = 1",
				balance-deductAmount,
			)
			if err != nil {
				return
			}

			if err := tx.Commit(ctx); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var final int
	pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = 1").Scan(&final)

	fmt.Printf("Succeeded          : %d\n", succeeded)
	fmt.Printf("Insufficient funds : %d\n", insufficient)
	fmt.Printf("Actual final       : %d\n", final)

	if final >= 0 {
		fmt.Println("CORRECT — SELECT FOR UPDATE serialised all transactions, no overdraft")
	} else {
		fmt.Printf("OVERDRAFT — balance went negative (%d)\n", final)
	}
}
