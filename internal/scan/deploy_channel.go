package scan

import (
	"errors"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeployChannel is mechanism ④ DEPLOY: deployment manifests naming which
// services a repository waits for (depends_on, Service selectors) and
// which services it itself deploys (compose names, workload app labels).
// Deployment references name *services*, resolved through the alias
// registry like BUILD/RUNTIME_CALL — self-declared, hence declared.
type DeployChannel struct{}

// NewDeployChannel returns the channel ready for RegisterChannel.
func NewDeployChannel() DeployChannel { return DeployChannel{} }

// Name is the channel id and the dedupe namespace for its evidence.
func (DeployChannel) Name() string { return "DEPLOY" }

// deployMaxFiles bounds upstream fetches per repository.
const deployMaxFiles = 10

// k8sWorkloadKinds whose app labels name the service this repo deploys;
// appLabelKeys are the legacy and recommended label conventions.
var (
	k8sWorkloadKinds = []string{"Deployment", "StatefulSet", "DaemonSet"}
	k8sAppLabelKeys  = []string{"app", "app.kubernetes.io/name"}
	// k8sManifestKeywords mark a YAML as a k8s manifest by filename;
	// deployDirNames are directories that conventionally hold manifests.
	k8sManifestKeywords = []string{"deployment", "statefulset", "daemonset", "service", "ingress"}
	deployDirNames      = []string{"k8s", "deploy", "deployments", "helm", "charts", "manifests"}
)

// isComposeFilename: docker-compose / compose manifests (*.yml/*.yaml).
func isComposeFilename(filename string) bool {
	name := strings.ToLower(basename(filename))
	return (strings.HasPrefix(name, "docker-compose") || strings.HasPrefix(name, "compose")) &&
		(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml"))
}

// isDeployFilename: compose by name; other manifests must be YAML and
// either carry a k8s keyword in the filename or live under a deployment
// directory. Helm values*.yaml are excluded — chart-specific data, not
// dependency signals.
func isDeployFilename(filename, path string) bool {
	name := strings.ToLower(basename(filename))
	if isComposeFilename(name) {
		return true
	}
	if strings.HasPrefix(name, "values") {
		return false
	}
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		return false
	}
	for _, segment := range strings.Split(strings.ToLower(path), "/") {
		for _, dir := range deployDirNames {
			if segment == dir {
				return true
			}
		}
	}
	for _, keyword := range k8sManifestKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}
	return false
}

// Select picks deployment manifests anywhere in the tree, sorted, capped.
func (DeployChannel) Select(tree []FileEntry) []string {
	var found []string
	for _, entry := range tree {
		if !entry.IsDir && isDeployFilename(entry.Path, entry.Path) {
			found = append(found, entry.Path)
		}
	}
	sort.Strings(found)
	if len(found) > deployMaxFiles {
		found = found[:deployMaxFiles]
	}
	return found
}

// Parse dispatches by filename: compose manifests by name, everything else
// by k8s kind inspection. Unknown content contributes nothing. Never fails.
func (DeployChannel) Parse(filename, content string) ChannelOutput {
	output := ChannelOutput{}
	targets, identities := parseDeployFile(basename(filename), content)
	for _, target := range targets {
		output.Evidence = append(output.Evidence, DepEvidence{
			Name:       target,
			Mechanism:  MechanismDeploy,
			Confidence: ConfidenceDeclared,
		})
	}
	output.DeployIdentities = identities
	return output
}

// parseDeployFile returns (targets, deploy identities).
func parseDeployFile(base, content string) ([]string, []string) {
	if isComposeFilename(base) {
		return parseCompose(content)
	}
	return parseK8s(content)
}

// parseCompose: every services.<name> is an identity this repository
// deploys; every depends_on entry (short list or long dict form) is a
// reference to another service.
func parseCompose(content string) ([]string, []string) {
	var targets, identities []string
	seenTargets := map[string]bool{}
	seenIdentities := map[string]bool{}

	for _, document := range yamlDocuments(content) {
		services := mappingValue(document, "services")
		if services == nil || services.Kind != yaml.MappingNode {
			continue
		}
		for index := 0; index+1 < len(services.Content); index += 2 {
			serviceName := services.Content[index].Value
			appendUniqueString(&identities, seenIdentities, serviceName)
			dependsOn := mappingValue(services.Content[index+1], "depends_on")
			for _, dep := range dependsOnNames(dependsOn) {
				appendUniqueString(&targets, seenTargets, dep)
			}
		}
	}
	return targets, identities
}

// dependsOnNames: depends_on as a sequence of names or a long-syntax
// mapping → names. A ${...} placeholder names no concrete service and is
// skipped.
func dependsOnNames(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	var names []string
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			names = append(names, item.Value)
		}
	case yaml.MappingNode:
		for index := 0; index < len(node.Content); index += 2 {
			names = append(names, node.Content[index].Value)
		}
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" && !strings.Contains(trimmed, "$") {
			kept = append(kept, trimmed)
		}
	}
	return kept
}

// parseK8s: Deployment/StatefulSet/DaemonSet workloads contribute their
// app label as an identity; a Service contributes metadata.name as an
// identity and its spec.selector app value as a target — the Service
// fronts that app. Multi-document streams handled.
func parseK8s(content string) ([]string, []string) {
	var targets, identities []string
	seenTargets := map[string]bool{}
	seenIdentities := map[string]bool{}

	for _, document := range yamlDocuments(content) {
		kind := scalarValue(mappingValue(document, "kind"))
		switch kind {
		case "Deployment", "StatefulSet", "DaemonSet":
			if label := workloadAppLabel(document); label != "" {
				appendUniqueString(&identities, seenIdentities, label)
			}
		case "Service":
			if name := metadataName(document); name != "" {
				appendUniqueString(&identities, seenIdentities, name)
			}
			if selector := serviceSelectorApp(document); selector != "" {
				appendUniqueString(&targets, seenTargets, selector)
			}
		}
	}
	return targets, identities
}

// workloadAppLabel reads spec.template.metadata.labels first (the pod
// labels, which is what selectors actually match), then metadata.labels.
func workloadAppLabel(document *yaml.Node) string {
	if labels := nestedNode(document, "spec", "template", "metadata", "labels"); labels != nil {
		if value := labelValue(labels); value != "" {
			return value
		}
	}
	return labelValue(nestedNode(document, "metadata", "labels"))
}

func serviceSelectorApp(document *yaml.Node) string {
	return labelValue(nestedNode(document, "spec", "selector"))
}

// labelValue: a concrete app value from a labels/selector mapping.
func labelValue(labels *yaml.Node) string {
	if labels == nil || labels.Kind != yaml.MappingNode {
		return ""
	}
	for index := 0; index+1 < len(labels.Content); index += 2 {
		key := labels.Content[index].Value
		recognized := false
		for _, appKey := range k8sAppLabelKeys {
			if key == appKey {
				recognized = true
				break
			}
		}
		if !recognized {
			continue
		}
		value := strings.TrimSpace(labels.Content[index+1].Value)
		if value != "" && !strings.Contains(value, "$") {
			return value
		}
	}
	return ""
}

func metadataName(document *yaml.Node) string {
	name := scalarValue(mappingValue(nestedNode(document, "metadata"), "name"))
	if name == "" || strings.Contains(name, "$") {
		return ""
	}
	return name
}

// nestedNode descends a mapping path, guarding non-mapping nodes along the
// way.
func nestedNode(document *yaml.Node, path ...string) *yaml.Node {
	node := document
	for _, part := range path {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		node = mappingValue(node, part)
	}
	return node
}

// ---------------------------------------------------------------------------
// shared YAML helpers
// ---------------------------------------------------------------------------

// yamlDocuments parses a multi-document YAML stream into documents,
// preserving key order for deterministic identities and targets. Never
// fails: a malformed stream yields nil.
func yamlDocuments(content string) []*yaml.Node {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var documents []*yaml.Node
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if !errors.Is(err, io.EOF) {
				return nil
			}
			break
		}
		if document.Kind == 0 || len(document.Content) == 0 {
			continue // empty document between --- separators
		}
		// A YAML document node wraps the root node — mount the root.
		documents = append(documents, document.Content[0])
	}
	return documents
}

// mappingValue returns the value node of a mapping entry, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// scalarValue reads a scalar node's text, trimmed.
func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func appendUniqueString(items *[]string, seen map[string]bool, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	key := strings.ToLower(trimmed)
	if seen[key] {
		return
	}
	seen[key] = true
	*items = append(*items, trimmed)
}
