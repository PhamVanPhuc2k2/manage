.DEFAULT_GOAL := help
SHELL := /bin/bash

# Nạp .env vào môi trường của make.
# Thiếu dòng này thì các target dùng $(POSTGRES_USER), $(PUBLIC_BASE_URL)...
# sẽ nhận chuỗi rỗng. Dấu - ở đầu để không lỗi khi chưa có .env.
-include .env
export

PUBLIC_BASE_URL ?= http://localhost

COMPOSE      := docker compose
COMPOSE_PROD := docker compose -f docker-compose.yml -f docker-compose.prod.yml

GIT_SHA    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: help
help: ## Hiện danh sách lệnh
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- môi trường
.PHONY: init
init: ## Thiết lập lần đầu: tạo .env, build image, khởi động
	@test -f .env || cp .env.example .env
	@$(COMPOSE) build
	@$(COMPOSE) up -d
	@echo ""
	@echo "Xong. Mở $(PUBLIC_BASE_URL)"
	@echo "Kiểm tra bằng: make smoke"

.PHONY: up
up: ## Khởi động toàn bộ (dev)
	$(COMPOSE) up -d

.PHONY: down
down: ## Dừng toàn bộ, GIỮ NGUYÊN dữ liệu
	$(COMPOSE) down

.PHONY: destroy
destroy: ## Dừng và XOÁ SẠCH dữ liệu (cẩn thận)
	@read -p "Xoá toàn bộ volume, mất hết dữ liệu. Gõ 'yes' để xác nhận: " ans; \
	if [ "$$ans" = "yes" ]; then $(COMPOSE) down -v; else echo "Đã huỷ"; fi

.PHONY: rebuild
rebuild: ## Build lại image của một service: make rebuild s=api
	$(COMPOSE) build $(s) && $(COMPOSE) up -d $(s)

.PHONY: restart
restart: ## Khởi động lại: make restart s=api
	$(COMPOSE) restart $(s)

.PHONY: logs
logs: ## Xem log: make logs s=api (bỏ s để xem tất cả)
	$(COMPOSE) logs -f --tail=100 $(s)

.PHONY: ps
ps: ## Trạng thái container
	$(COMPOSE) ps

.PHONY: sh
sh: ## Vào shell container: make sh s=api
	$(COMPOSE) exec $(s) sh

# ------------------------------------------------------------------ migration
.PHONY: migrate
migrate: ## Chạy migration
	$(COMPOSE) run --rm migrate

.PHONY: migrate-down
migrate-down: ## Lùi 1 bước migration
	$(COMPOSE) run --rm migrate \
		-path=/migrations \
		-database="postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@postgres:5432/$${POSTGRES_DB}?sslmode=disable" \
		down 1

.PHONY: migrate-create
migrate-create: ## Tạo migration mới: make migrate-create n=create_users
	$(COMPOSE) run --rm --entrypoint migrate migrate \
		create -ext sql -dir /migrations -seq $(n)

# ----------------------------------------------------- kiểm thử & chất lượng
.PHONY: test
test: ## Chạy unit test
	$(COMPOSE) exec -T api sh -c "cd /app && go test ./... -race -count=1"

.PHONY: cover
cover: ## Chạy test kèm báo cáo coverage
	$(COMPOSE) exec -T api sh -c \
		"cd /app && go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1"

.PHONY: lint
lint: ## Kiểm tra code Go
	docker run --rm -v "$$(pwd)/backend:/app" -w /app \
		golangci/golangci-lint:v2.13.2 golangci-lint run ./... --timeout 5m

.PHONY: tidy
tidy: ## Dọn go.mod
	$(COMPOSE) exec -T api sh -c "cd /app && go mod tidy"

.PHONY: fmt
fmt: ## Format code Go
	$(COMPOSE) exec -T api sh -c "cd /app && gofmt -l -w ."

# ----------------------------------------------------------------- tiện ích
.PHONY: psql
psql: ## Mở psql
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER} -d $${POSTGRES_DB}

.PHONY: redis-cli
redis-cli: ## Mở redis-cli
	$(COMPOSE) exec redis redis-cli -a $${REDIS_PASSWORD}

.PHONY: smoke
smoke: ## Kiểm tra nhanh toàn hệ thống sau khi up
	@echo "--- nginx ---"
	@curl -fsS $(PUBLIC_BASE_URL)/health | head -c 200; echo
	@echo "--- api /ready (postgres + redis + rabbitmq) ---"
	@curl -fsS http://localhost:$(API_PORT)/ready | head -c 300; echo
	@echo "--- /api/v1/ping qua nginx ---"
	@curl -fsS $(PUBLIC_BASE_URL)/api/v1/ping | head -c 400; echo
	@echo ""
	@echo "--- log worker (job vừa nhận) ---"
	@sleep 2; $(COMPOSE) logs --tail=20 worker | grep "worker đã xử lý job" | tail -1 \
		|| echo "CHƯA THẤY JOB — kiểm tra bằng: make logs s=worker"

.PHONY: queues
queues: ## Xem hàng đợi RabbitMQ (kiểm tra dead-letter)
	@$(COMPOSE) exec -T rabbitmq rabbitmqctl list_queues name messages consumers 2>/dev/null | grep manage

# -------------------------------------------------------------- production
.PHONY: prod-up
prod-up: ## Khởi động production
	$(COMPOSE_PROD) up -d

.PHONY: prod-logs
prod-logs: ## Log production
	$(COMPOSE_PROD) logs -f --tail=100 $(s)

.PHONY: build-images
build-images: ## Build image production tại máy
	docker build -f docker/backend/Dockerfile --target prod \
		--build-arg BINARY=api --build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t ghcr.io/yourorg/manage-api:$(GIT_SHA) ./backend
	docker build -f docker/backend/Dockerfile --target prod \
		--build-arg BINARY=worker --build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t ghcr.io/yourorg/manage-worker:$(GIT_SHA) ./backend
	docker build -f docker/frontend/Dockerfile --target prod \
		-t ghcr.io/yourorg/manage-frontend:$(GIT_SHA) ./frontend
