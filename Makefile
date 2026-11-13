.PHONY: test build migrate api web crawler judge docker-up docker-down
test:
	go test ./...
	pnpm test
	cd apps/judge-worker && uv run pytest
build:
	go build ./cmd/api
	pnpm build
migrate:
	go run ./cmd/migrate up
api:
	go run ./cmd/api
web:
	pnpm dev:web
crawler:
	pnpm dev:crawler
judge:
	cd apps/judge-worker && uv run uvicorn geo_judge.app:app --reload --port 8081
docker-up:
	docker compose up --build
docker-down:
	docker compose down
