module github.com/devpablocristo/companion

go 1.26.1

require (
	github.com/devpablocristo/platform/authn/go v0.2.0
	github.com/devpablocristo/platform/concurrency/go v0.1.1
	github.com/devpablocristo/platform/config/go v0.1.0
	github.com/devpablocristo/platform/contracts/ai/go v0.1.0
	github.com/devpablocristo/platform/databases/postgres/go v0.1.0
	github.com/devpablocristo/platform/errors/go v0.2.0
	github.com/devpablocristo/platform/http/go v0.1.0
	github.com/devpablocristo/platform/kernels/ai/go v0.1.0
	github.com/devpablocristo/platform/lifecycle/go v0.0.0
	github.com/devpablocristo/platform/observability/go v0.1.0
	github.com/devpablocristo/platform/security/go v0.2.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.9.2
	golang.org/x/oauth2 v0.36.0
)

replace github.com/devpablocristo/platform/authn/go => ../../platform/authn/go

replace github.com/devpablocristo/platform/lifecycle/go => ../../platform/lifecycle/go

require (
	cloud.google.com/go/compute/metadata v0.8.0 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gorm.io/driver/mysql v1.6.0 // indirect
	gorm.io/driver/postgres v1.5.11 // indirect
	gorm.io/driver/sqlite v1.6.0 // indirect
	gorm.io/gorm v1.30.0 // indirect
)
