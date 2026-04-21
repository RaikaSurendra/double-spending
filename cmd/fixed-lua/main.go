// Lua scripts run atomically inside Redis — no other command can execute
// between any two lines of the script. This lets you express conditional
// read-modify-write logic (e.g. "only deduct if sufficient funds") without
// a race condition.
//
// Scenario: balance starts at 500, but 100 goroutines each try to deduct 10.
// Total attempted deduction (1000) exceeds available funds (500).
// Without atomicity: balance goes negative (overdraft / double-spend).
// With Lua: exactly 50 succeed, 50 get INSUFFICIENT_FUNDS — balance lands at 0.
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"
)

const (
	key          = "balance:lua"
	initialFunds = 500
	goroutines   = 100
	deductAmount = 10
)

// deductScript checks the balance and deducts atomically.
// Returns the new balance, or an error if funds are insufficient.
var deductScript = redis.NewScript(`
local balance = tonumber(redis.call('GET', KEYS[1]))
local amount  = tonumber(ARGV[1])
if balance < amount then
    return redis.error_reply('INSUFFICIENT_FUNDS')
end
return redis.call('DECRBY', KEYS[1], amount)
`)

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()

	rdb.Set(ctx, key, initialFunds, 0)
	fmt.Printf("Initial balance    : %d\n", initialFunds)
	fmt.Printf("Goroutines         : %d (each tries to deduct %d)\n", goroutines, deductAmount)
	fmt.Printf("Total attempted    : %d  (exceeds available funds)\n", goroutines*deductAmount)
	fmt.Printf("Expected final     : 0  (no overdraft)\n\n")

	var (
		wg          sync.WaitGroup
		succeeded   atomic.Int64
		insufficient atomic.Int64
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := deductScript.Run(ctx, rdb, []string{key}, deductAmount).Err()
			if err != nil {
				if strings.Contains(err.Error(), "INSUFFICIENT_FUNDS") {
					insufficient.Add(1)
				}
			} else {
				succeeded.Add(1)
			}
		}()
	}
	wg.Wait()

	final, _ := rdb.Get(ctx, key).Int()

	fmt.Printf("Succeeded          : %d\n", succeeded.Load())
	fmt.Printf("Insufficient funds : %d\n", insufficient.Load())
	fmt.Printf("Actual final       : %d\n", final)

	if final >= 0 {
		fmt.Println("CORRECT — Lua script prevented overdraft atomically")
	} else {
		fmt.Printf("OVERDRAFT — balance went negative (%d)\n", final)
	}
}
