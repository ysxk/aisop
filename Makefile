.PHONY: build fmt fmt-check vet test integration-test run migrate bootstrap-admin

build:
	mkdir -p bin
	go build -trimpath -o bin/aisop ./cmd/aisop

fmt:
	find cmd internal api tests -type f -name '*.go' -exec gofmt -w {} +

fmt-check:
	@files="$$(find cmd internal api tests -type f -name '*.go' -exec gofmt -l {} +)"; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

vet:
	go vet ./...

test:
	go test -race ./...

integration-test:
	@test -n "$$TEST_DATABASE_URL" || { printf '%s\n' 'TEST_DATABASE_URL is required (use a dedicated test database)'; exit 1; }
	go test -race -count=1 ./...

run:
	go run ./cmd/aisop serve

migrate:
	go run ./cmd/aisop migrate

bootstrap-admin:
	go run ./cmd/aisop bootstrap-admin
