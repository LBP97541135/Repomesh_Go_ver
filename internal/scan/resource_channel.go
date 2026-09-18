package scan

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResourceChannel is mechanism ③ SHARED_RESOURCE: application configuration
// naming the database/Redis/MQ/bucket a repository *shares* with others.
// The identifier names a resource, not a repository, so the graph never
// resolves it through the service registry; two repositories declaring the
// same identifier simply share that resource. Self-declared configuration,
// hence confidence=declared, and never a free-text dep.
type ResourceChannel struct{}

// NewResourceChannel returns the channel ready for RegisterChannel.
func NewResourceChannel() ResourceChannel { return ResourceChannel{} }

// Name is the channel id and the dedupe namespace for its evidence.
func (ResourceChannel) Name() string { return "SHARED_RESOURCE" }

// resourceMaxFiles bounds upstream fetches per repository.
const resourceMaxFiles = 10

// Select picks application configuration files anywhere in the tree:
// application*.yml/.yaml/.properties and bootstrap.y*ml. ".env" files are
// deliberately excluded — local environment overrides often carry secrets
// and are deployment config, not shared-state evidence.
func (ResourceChannel) Select(tree []FileEntry) []string {
	var found []string
	for _, entry := range tree {
		if entry.IsDir {
			continue
		}
		name := strings.ToLower(basename(entry.Path))
		if strings.HasPrefix(name, "application") && strings.HasSuffix(name, ".properties") {
			found = append(found, entry.Path)
			continue
		}
		if strings.HasPrefix(name, "application") &&
			(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			found = append(found, entry.Path)
			continue
		}
		if name == "bootstrap.yml" || name == "bootstrap.yaml" {
			found = append(found, entry.Path)
		}
	}
	sort.Strings(found)
	if len(found) > resourceMaxFiles {
		found = found[:resourceMaxFiles]
	}
	return found
}

// Parse dispatches by extension: YAML (multi-document), Java properties,
// and dotenv shapes. Unknown kinds contribute nothing. Never fails.
func (ResourceChannel) Parse(filename, content string) ChannelOutput {
	output := ChannelOutput{}
	for _, identifier := range parseResourceConfig(basename(filename), content) {
		output.Evidence = append(output.Evidence, DepEvidence{
			Name:       identifier,
			Mechanism:  MechanismSharedResource,
			Confidence: ConfidenceDeclared,
		})
	}
	return output
}

// kv is one flat key/value pair, in document order. Order matters: Python's
// dict preserved insertion order, and the evidence sequence of a card is
// compared field-for-field against the golden fixtures.
type kv struct{ key, value string }

// parseResourceConfig maps one config file to namespaced resource
// identifiers ("DATABASE:orders-db"). Shared with nothing else: the
// parsing rules live here so the graph's evidence stays uniform.
func parseResourceConfig(base, content string) []string {
	if strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml") {
		return extractIdentifiers(yamlIdentifiers(content))
	}
	if strings.HasSuffix(base, ".properties") {
		return extractIdentifiers(parseKeyValue(content, true))
	}
	if strings.HasSuffix(base, ".env") {
		return extractIdentifiers(parseKeyValue(content, false))
	}
	return nil
}

// dbSchemes: "jdbc:" covers jdbc:mysql and friends; bare schemes cover
// non-JDBC drivers.
var dbSchemes = []string{"jdbc:", "mysql://", "postgres://", "postgresql://", "mariadb://"}

// extractIdentifiers maps flat key/value pairs to normalised identifiers.
// Keys are matched after lower-casing and mapping "_" to ".", which lets
// one rule set cover dot-separated properties/YAML keys and
// SCREAMING_SNAKE env names (SPRING_REDIS_HOST → spring.redis.host).
// Iteration follows document order and dedupes case-insensitively, first
// occurrence wins.
func extractIdentifiers(pairs []kv) []string {
	normalized := make(map[string]string, len(pairs))
	var keys []string
	for _, pair := range pairs {
		key := strings.ReplaceAll(strings.ToLower(pair.key), "_", ".")
		if _, tracked := normalized[key]; !tracked {
			keys = append(keys, key)
		}
		normalized[key] = pair.value
	}

	var found []string
	seen := map[string]bool{}
	for _, key := range keys {
		value := strings.TrimSpace(normalized[key])
		// An unresolved placeholder (${DB_URL}, ${REDIS_HOST:-cache}) is not
		// a concrete resource; "$" never legitimately appears in a host,
		// database or bucket name.
		if value == "" || strings.Contains(value, "$") {
			continue
		}
		identifier := identifierFor(key, value, normalized)
		if identifier == "" {
			continue
		}
		if seen[strings.ToLower(identifier)] {
			continue
		}
		seen[strings.ToLower(identifier)] = true
		found = append(found, identifier)
	}
	return found
}

// identifierFor derives one resource identifier for a key/value pair, or
// "" when the pair names no shared resource.
func identifierFor(key, value string, flat map[string]string) string {
	switch {
	case strings.HasSuffix(key, ".url") &&
		(strings.Contains(key, "datasource") || key == "db.url" || key == "database.url"):
		if database := databaseName(value); database != "" {
			return "DATABASE:" + database
		}
	case strings.Contains(key, "redis") && strings.HasSuffix(key, ".url") && strings.Contains(value, "://"):
		if hostPort := hostPort(value); hostPort != "" {
			return "REDIS:" + hostPort
		}
	case strings.Contains(key, "redis") && strings.HasSuffix(key, ".host"):
		hostPort := hostPort(value)
		// A value already carrying its port is the full identifier; a
		// sibling *.port key is only appended when the host alone would
		// lose it. Never cache-01:6379:6379.
		if strings.Contains(hostPort, ":") {
			return "REDIS:" + hostPort
		}
		if port := siblingPort(flat, key); port != "" {
			return "REDIS:" + hostPort + ":" + port
		}
		return "REDIS:" + hostPort
	case strings.Contains(key, "kafka") &&
		(strings.HasSuffix(key, ".bootstrap.servers") || strings.HasSuffix(key, ".bootstrap-servers")):
		return "MQ:" + value
	case strings.Contains(key, "rabbitmq") && strings.HasSuffix(key, ".host"):
		hostPort := hostPort(value)
		if strings.Contains(hostPort, ":") {
			return "MQ:" + hostPort
		}
		if port := siblingPort(flat, key); port != "" {
			return "MQ:" + hostPort + ":" + port
		}
		return "MQ:" + hostPort
	case strings.Contains(key, "rocketmq") &&
		(strings.HasSuffix(key, ".name-server") || strings.HasSuffix(key, ".name.server")):
		return "MQ:" + value
	case strings.Contains(key, "bucket"):
		return "BUCKET:" + value
	}
	return ""
}

// databaseName extracts the database instance name from a JDBC or driver
// URL: "jdbc:mysql://host:3306/orders-db?useSSL=false" → "orders-db".
// URLs without a database path and in-memory H2 name no shared resource.
func databaseName(value string) string {
	value = strings.TrimSpace(value)
	scheme := false
	for _, prefix := range dbSchemes {
		if strings.HasPrefix(value, prefix) {
			scheme = true
			break
		}
	}
	if !scheme || !strings.Contains(value, "://") || strings.Contains(value, "h2:mem") {
		return ""
	}
	body := strings.SplitN(value, "://", 2)[1]
	if !strings.Contains(body, "/") {
		return ""
	}
	path := strings.SplitN(body, "/", 2)[1]
	for _, separator := range []string{"?", ";", "#"} {
		path = strings.SplitN(path, separator, 2)[0]
	}
	return strings.Trim(strings.TrimSpace(path), "/")
}

// hostPort normalises a host[:port] value: strip scheme, path and query.
// "redis://cache-01:6379/0" → "cache-01:6379"; "rabbit-01" → "rabbit-01".
func hostPort(value string) string {
	hostPort := strings.TrimSpace(value)
	if index := strings.Index(hostPort, "://"); index >= 0 {
		hostPort = hostPort[index+3:]
	}
	if index := strings.Index(hostPort, "/"); index >= 0 {
		hostPort = hostPort[:index]
	}
	if index := strings.Index(hostPort, "?"); index >= 0 {
		hostPort = hostPort[:index]
	}
	return strings.TrimSpace(hostPort)
}

// siblingPort reads the explicit port from the sibling *.port key
// ("spring.redis.host" looks for "spring.redis.port").
func siblingPort(flat map[string]string, key string) string {
	return strings.TrimSpace(flat[strings.TrimSuffix(key, "host")+"port"])
}

// ---------------------------------------------------------------------------
// YAML (multi-document, application.yml commonly splits env profiles with ---)
// ---------------------------------------------------------------------------

func yamlIdentifiers(content string) []kv {
	var flat []kv
	decoder := yaml.NewDecoder(strings.NewReader(content))
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if !errors.Is(err, io.EOF) {
				return nil // a malformed stream yields nothing, never a failure
			}
			break
		}
		if node.Kind == 0 {
			continue // empty document between --- separators
		}
		flattenYAML(&node, "", &flat)
	}
	return flat
}

// flattenYAML walks a yaml.Node tree preserving document order (mapping
// pairs in key order, sequences indexed) into dot-separated leaf pairs.
// Only scalar leaves are kept; nulls name nothing.
func flattenYAML(node *yaml.Node, prefix string, out *[]kv) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) > 0 {
			flattenYAML(node.Content[0], prefix, out)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flattenYAML(node.Content[index+1], path, out)
		}
	case yaml.SequenceNode:
		for index, item := range node.Content {
			flattenYAML(item, fmt.Sprintf("%s[%d]", prefix, index), out)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return
		}
		*out = append(*out, kv{key: prefix, value: node.Value})
	}
}

// ---------------------------------------------------------------------------
// properties / dotenv
// ---------------------------------------------------------------------------

// parseKeyValue parses a key=value file, optionally also accepting
// "key: value" (Java properties). "#" and "!" start comments; env values
// strip matching quotes and inline comments; continuation lines are out of
// scope for application config.
func parseKeyValue(content string, colon bool) []kv {
	var flat []kv
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if !colon && strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		var key, value string
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else if colon && strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else {
			continue
		}
		if key == "" {
			continue
		}
		if colon {
			// Java properties: a '#' preceded by whitespace starts a comment.
			if hashIndex := strings.Index(value, " #"); hashIndex >= 0 {
				value = strings.TrimSpace(value[:hashIndex])
			}
		} else {
			if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
				value = value[1 : len(value)-1]
			} else if hashIndex := strings.Index(value, " #"); hashIndex >= 0 {
				value = strings.TrimSpace(value[:hashIndex])
			}
		}
		flat = append(flat, kv{key: key, value: value})
	}
	return flat
}
