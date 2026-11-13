.PHONY: test build up down migrate
test:
	go test ./...
	pnpm test
	cd apps/judge-worker && uv run pytest
build:
	go build ./cmd/api
	pnpm build
up:
	docker compose up --build
down:
	docker compose down
migrate:
	docker compose run --rm migrate
