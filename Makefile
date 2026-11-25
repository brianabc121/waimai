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

# Run back to back, the tests sign in faster than the API allows from one
# address; e2e/patient-login.cjs makes every browser retry a refused login.
E2E_NODE = node -r ./e2e/patient-login.cjs
e2e:
	$(E2E_NODE) e2e/flow.cjs
	$(E2E_NODE) e2e/dinein.cjs
	$(E2E_NODE) e2e/notetemplates.cjs
	$(E2E_NODE) e2e/templates.cjs
	$(E2E_NODE) e2e/admin.cjs
	$(E2E_NODE) e2e/dinedesk.cjs
	$(E2E_NODE) e2e/void.cjs
	$(E2E_NODE) e2e/toppings.cjs
	$(E2E_NODE) e2e/mapzones.cjs
	$(E2E_NODE) e2e/mapgoogle.cjs
	$(E2E_NODE) e2e/staff.cjs
	$(E2E_NODE) e2e/entry.cjs
	$(E2E_NODE) e2e/approve.cjs
	$(E2E_NODE) e2e/duty.cjs
	$(E2E_NODE) e2e/attendance.cjs
	$(E2E_NODE) e2e/kitchenreport.cjs
	$(E2E_NODE) e2e/progress.cjs
	$(E2E_NODE) e2e/checkout.cjs
	$(E2E_NODE) e2e/riderreport.cjs
	$(E2E_NODE) e2e/staffexport.cjs
	$(E2E_NODE) e2e/settings.cjs
	$(E2E_NODE) e2e/responsive.cjs
	$(E2E_NODE) e2e/manualpic.cjs
	$(E2E_NODE) e2e/storefront.cjs
	$(E2E_NODE) e2e/manualorder.cjs
	$(E2E_NODE) e2e/addon.cjs
	$(E2E_NODE) e2e/members.cjs
	$(E2E_NODE) e2e/addressbook.cjs
	$(E2E_NODE) e2e/deliverystats.cjs
	$(E2E_NODE) e2e/menuimport.cjs
	$(E2E_NODE) e2e/coupons.cjs
	$(E2E_NODE) e2e/coupon_batches.cjs
	$(E2E_NODE) e2e/bills.cjs
	$(E2E_NODE) e2e/addressform.cjs
	$(E2E_NODE) e2e/account.cjs
	$(E2E_NODE) e2e/chat.cjs
	$(E2E_NODE) e2e/support.cjs
	$(E2E_NODE) e2e/me.cjs
	$(E2E_NODE) e2e/orders.cjs
	$(E2E_NODE) e2e/media.cjs
	$(E2E_NODE) e2e/mall.cjs
	$(E2E_NODE) e2e/dishpage.cjs
	$(E2E_NODE) e2e/currency.cjs
	$(E2E_NODE) e2e/i18n.cjs
	$(E2E_NODE) e2e/menutranslate.cjs
	$(E2E_NODE) e2e/i18nadmin.cjs
	$(E2E_NODE) e2e/zawgyi.cjs
	$(E2E_NODE) e2e/longtext.cjs
