.PHONY: test build web run migrate seed e2e

# Tests need a real PostgreSQL: TEST_DATABASE_URL=postgres://.../waimai_test
test:
	go vet ./...
	go test -count=1 -p 1 ./...

build:
	go build -o bin/ ./cmd/...

web:
	cd web && npm ci --no-audit --no-fund && npm run build

migrate:
	go run ./cmd/migrate

seed:
	go run ./cmd/seed

# e2e/mapzones.cjs needs the API started with GOOGLE_MAPS_API_KEY=stub (a test map without Google).
# Single process for local use: API + worker + built frontend on :8080.
run: build
	STATIC_DIR=web/dist EMBED_WORKER=true OTP_DEV_ECHO=true ./bin/api

e2e:
	node e2e/flow.cjs
	node e2e/dinein.cjs
	node e2e/templates.cjs
	node e2e/admin.cjs
	node e2e/mapzones.cjs
	node e2e/staff.cjs
	node e2e/duty.cjs
	node e2e/attendance.cjs
	node e2e/kitchenreport.cjs
