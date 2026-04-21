.PHONY: up down demo-broken demo-mutex-wrong demo-fixed-incrby demo-fixed-lua demo-all

up:
	docker compose up -d
	@echo "Redis is up on localhost:6379"

down:
	docker compose down

demo-broken:
	@echo "\n=== DEMO 1: Broken — non-atomic GET+SET ==="
	go run ./cmd/broken

demo-mutex-wrong:
	@echo "\n=== DEMO 2: Wrong Fix — Mutex does not protect distributed state ==="
	go run ./cmd/mutex-wrong

demo-fixed-incrby:
	@echo "\n=== DEMO 3: Fixed — atomic DECRBY ==="
	go run ./cmd/fixed-incrby

demo-fixed-lua:
	@echo "\n=== DEMO 4: Fixed — Lua script with overdraft protection ==="
	go run ./cmd/fixed-lua

demo-all: demo-broken demo-mutex-wrong demo-fixed-incrby demo-fixed-lua
