.PHONY: dev web core core-postgres edge db-install db-up db-down db-test test check contracts release-check

dev:
	@echo "Run 'make core', 'make edge', and 'make web' in separate terminals"

web:
	npm run dev:connected

core:
	npm run core:dev

core-postgres:
	npm run core:postgres

edge:
	npm run edge:dev

db-install:
	npm run db:install

db-up:
	npm run db:up

db-down:
	npm run db:down

db-test:
	npm run db:test

test:
	npm run test
	npm run go:test

contracts:
	npm run contracts:validate

check:
	npm run verify

release-check:
	npm run release:check
