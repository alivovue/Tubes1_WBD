module schakker_tests

go 1.26.0

toolchain go1.26.10

require (
	github.com/go-sql-driver/mysql v1.9.3
	github.com/jackc/pgx/v5 v5.11.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/trace v1.47.0
	golang.org/x/crypto v0.58.0
	schakker_shared v0.0.0
	tubes1_wbd v0.0.0
	tubes1_wbd_admin v0.0.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace tubes1_wbd => ../src

replace tubes1_wbd_admin => ../src-admin

replace schakker_shared => ../shared/backend
