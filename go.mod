module github.com/ISch-1/url-shortener

go 1.23

require (
	github.com/jackc/pgx/v5 v5.7.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/prometheus/client_golang v1.20.4
	github.com/redis/go-redis/v9 v9.7.0
	github.com/stretchr/testify v1.9.0
	github.com/testcontainers/testcontainers-go v0.33.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.33.0
	github.com/testcontainers/testcontainers-go/modules/redis v0.33.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.55.0
	go.opentelemetry.io/otel v1.30.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.30.0
	go.opentelemetry.io/otel/sdk v1.30.0
	go.opentelemetry.io/otel/trace v1.30.0
	golang.org/x/sync v0.8.0
	google.golang.org/grpc v1.66.2
	google.golang.org/protobuf v1.34.2
)
