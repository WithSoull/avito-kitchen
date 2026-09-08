.PHONY: run-platform run-venue test test-cover vet lint openapi-lint compose-up compose-down compose-config migration-test e2e demo diagrams audit

run-platform:
	go run ./cmd/avito-kitchen

run-venue:
	go run ./cmd/venue

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...

vet:
	go vet ./...

lint:
	docker run --rm -v "$(CURDIR):/app" -w /app golangci/golangci-lint:v2.12.2 golangci-lint run

openapi-lint:
	npx --yes @redocly/cli@1.34.5 lint api/openapi.yaml

compose-up:
	docker compose up --build

compose-down:
	docker compose down

compose-config:
	docker compose config --quiet

migration-test:
	./scripts/test-migrations.sh

e2e:
	./scripts/e2e.sh

demo: e2e

diagrams:
	./scripts/render-diagrams.sh

audit:
	./scripts/audit-repository.sh
