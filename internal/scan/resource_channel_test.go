package scan

import (
	"fmt"
	"testing"
)

func TestResourceChannelSpringDatasourceYAML(t *testing.T) {
	content := `
spring:
  datasource:
    url: jdbc:mysql://orders-db:3306/orders?useSSL=false
  redis:
    host: cache-01
    port: 6379
app:
  kafka:
    bootstrap-servers: kafka-01:9092
`
	output := ResourceChannel{}.Parse("application.yml", content)
	names := evidenceNames(output.Evidence)

	// The database name is the URL path segment ("orders"), not the host.
	want := []string{"DATABASE:orders", "REDIS:cache-01:6379", "MQ:kafka-01:9092"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("identifiers = %v, want %v", names, want)
	}
	for _, evidence := range output.Evidence {
		if evidence.Mechanism != MechanismSharedResource || evidence.Confidence != ConfidenceDeclared {
			t.Fatalf("evidence = %+v", evidence)
		}
	}
	// Shared resources never become free-text deps.
	if len(output.Deps) != 0 {
		t.Fatalf("shared resources must not enter deps: %v", output.Deps)
	}
}

func TestResourceChannelMultiDocumentAndProperties(t *testing.T) {
	multi := `
spring:
  datasource:
    url: jdbc:mysql://host:3306/orders
---
spring:
  datasource:
    url: jdbc:postgresql://billing-db:5432/billing
`
	output := ResourceChannel{}.Parse("application-multidoc.yml", multi)
	// Documents merge through a dict update: the same key in a later
	// profile overwrites the earlier value (Python dict.update semantics).
	if got := evidenceNames(output.Evidence); fmt.Sprint(got) != fmt.Sprint([]string{"DATABASE:billing"}) {
		t.Fatalf("--- documents must merge by overwrite, got %v", got)
	}

	properties := ResourceChannel{}.Parse("application.properties", `
db.url = jdbc:mysql://billing-db:3306/billing
spring.redis.host = redis-cache
# spring.redis.port missing: host alone is the identifier
some.random.key = ignored
`)
	want := []string{"DATABASE:billing", "REDIS:redis-cache"}
	if fmt.Sprint(evidenceNames(properties.Evidence)) != fmt.Sprint(want) {
		t.Fatalf("properties identifiers = %v, want %v", evidenceNames(properties.Evidence), want)
	}
}

func TestResourceChannelRedisURLAndRabbitMQAndBucket(t *testing.T) {
	flat := []kv{
		{key: "spring.redis.host", value: "redis://cache-01:6379/0"},
		{key: "rabbitmq.host", value: "rabbit-01"},
		{key: "storage.bucket.name", value: "invoices"},
		{key: "rocketmq.name-server", value: "mq-01:9876"},
		{key: "datasource.primary.url", value: "postgres://user:pass@db-01:5432/orders"},
	}
	got := extractIdentifiers(flat)
	want := []string{
		"REDIS:cache-01:6379",
		"MQ:rabbit-01",
		"BUCKET:invoices",
		"MQ:mq-01:9876",
		"DATABASE:orders",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("identifiers = %v, want %v", got, want)
	}
}

func TestResourceChannelSkipsPlaceholdersAndInMemory(t *testing.T) {
	flat := []kv{
		{key: "spring.datasource.url", value: "${DB_URL}"},
		{key: "db.url", value: "jdbc:h2:mem:testdb"},
		{key: "spring.redis.host", value: ""},
	}
	if got := stringSliceAsEvidence(extractIdentifiers(flat)); len(got) != 0 {
		t.Fatalf("placeholders, in-memory H2 and empty values name nothing: %v", got)
	}
}

func TestResourceChannelPropertiesCommentAndQuotes(t *testing.T) {
	const content = "db.url = jdbc:mysql://orders-db:3306/orders # primary database\n"
	if got := parseResourceConfig("application.properties", content); fmt.Sprint(got) != fmt.Sprint([]string{"DATABASE:orders"}) {
		t.Fatalf("trailing comment must be stripped: %v", got)
	}
}

func TestResourceChannelSelectKnownFilesOnly(t *testing.T) {
	tree := []FileEntry{
		{Path: "src/main/resources/application.yml", IsDir: false},
		{Path: "src/main/resources/application-prod.yaml", IsDir: false},
		{Path: "src/main/resources/application-dev.properties", IsDir: false},
		{Path: ".env", IsDir: false},       // deliberately excluded
		{Path: "random.yml", IsDir: false}, // not application config
	}
	selected := ResourceChannel{}.Select(tree)
	if len(selected) != 3 {
		t.Fatalf("selected = %v (.env and random.yml must stay out; bootstrap.properties is not selected either)", selected)
	}
}

func evidenceNames(evidence []DepEvidence) []string {
	names := make([]string, 0, len(evidence))
	for _, item := range evidence {
		names = append(names, item.Name)
	}
	return names
}

// stringSliceAsEvidence wraps plain names so the shared name-printing helper
// can render them; assertions only inspect .Name.
func stringSliceAsEvidence(names []string) []DepEvidence {
	out := make([]DepEvidence, 0, len(names))
	for _, name := range names {
		out = append(out, DepEvidence{Name: name})
	}
	return out
}
