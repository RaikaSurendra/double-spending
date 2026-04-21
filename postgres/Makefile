.PHONY: up down wait demo-broken demo-mutex-wrong demo-fixed-atomic demo-fixed-sfu demo-fixed-optimistic demo-all

up:
	docker compose up -d
	@echo "Waiting for Postgres to be ready..."
	@until docker exec double-spending-postgres pg_isready -U postgres -q; do sleep 1; done
	@echo "Postgres is ready on localhost:5432"

down:
	docker compose down

demo-broken:
	@echo "\n=== DEMO 1: Broken — non-atomic SELECT + UPDATE ==="
	go run ./cmd/broken

demo-mutex-wrong:
	@echo "\n=== DEMO 2: Wrong Fix — Mutex does not protect distributed state ==="
	go run ./cmd/mutex-wrong

demo-fixed-atomic:
	@echo "\n=== DEMO 3: Fixed — atomic UPDATE balance = balance - N ==="
	go run ./cmd/fixed-atomic

demo-fixed-sfu:
	@echo "\n=== DEMO 4: Fixed — SELECT FOR UPDATE (pessimistic row lock) ==="
	go run ./cmd/fixed-select-for-update

demo-fixed-optimistic:
	@echo "\n=== DEMO 5: Fixed — Optimistic locking with version column ==="
	go run ./cmd/fixed-optimistic

demo-all: demo-broken demo-mutex-wrong demo-fixed-atomic demo-fixed-sfu demo-fixed-optimistic
