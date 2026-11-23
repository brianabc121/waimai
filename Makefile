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
	node e2e/mapgoogle.cjs
	node e2e/staff.cjs
	node e2e/duty.cjs
	node e2e/attendance.cjs
	node e2e/kitchenreport.cjs
	node e2e/riderreport.cjs
	node e2e/staffexport.cjs
	node e2e/settings.cjs
	node e2e/responsive.cjs
	node e2e/storefront.cjs
	node e2e/menuimport.cjs
	node e2e/coupons.cjs
	node e2e/coupon_batches.cjs
	node e2e/bills.cjs
	node e2e/addressform.cjs
	node e2e/account.cjs
	node e2e/chat.cjs
	node e2e/support.cjs
	node e2e/me.cjs
	node e2e/orders.cjs
	node e2e/media.cjs
	node e2e/mall.cjs
	node e2e/dishpage.cjs
	node e2e/currency.cjs
	node e2e/i18n.cjs
	node e2e/menutranslate.cjs
	node e2e/i18nadmin.cjs
	node e2e/zawgyi.cjs
