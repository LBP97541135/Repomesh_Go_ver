package scan

import (
	"fmt"
	"testing"
)

func TestDeployChannelCompose(t *testing.T) {
	content := `
services:
  kitchen-sink:
    image: com.demo/kitchen-sink
    depends_on:
      - ts-payment-service
      - ${optional-dependency}
  orders-db:
    image: mysql:8
`
	output := DeployChannel{}.Parse("docker-compose.yml", content)

	if len(output.Evidence) != 1 ||
		output.Evidence[0].Name != "ts-payment-service" ||
		output.Evidence[0].Mechanism != MechanismDeploy ||
		output.Evidence[0].Confidence != ConfidenceDeclared {
		t.Fatalf("evidence = %+v (placeholder names nothing)", output.Evidence)
	}
	// Every compose service name is an identity this repo deploys.
	identities := map[string]bool{}
	for _, identity := range output.DeployIdentities {
		identities[identity] = true
	}
	if !identities["kitchen-sink"] || !identities["orders-db"] {
		t.Fatalf("identities = %v", output.DeployIdentities)
	}
}

func TestDeployChannelComposeLongSyntaxDependsOn(t *testing.T) {
	content := `
services:
  api:
    depends_on:
      orders-db:
        condition: service_healthy
`
	output := DeployChannel{}.Parse("compose.yaml", content)
	if len(output.Evidence) != 1 || output.Evidence[0].Name != "orders-db" {
		t.Fatalf("long syntax depends_on = %+v", output.Evidence)
	}
}

func TestDeployChannelK8sWorkloadAndService(t *testing.T) {
	content := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders
spec:
  template:
    metadata:
      labels:
        app: orders-service
---
apiVersion: v1
kind: Service
metadata:
  name: orders-service
spec:
  selector:
    app.kubernetes.io/name: orders-service
`
	output := DeployChannel{}.Parse("k8s/orders.yaml", content)

	// The workload's pod label and the Service's own name are identities;
	// the Service selector is a target (the Service fronts that app).
	targetNames := evidenceNames(output.Evidence)
	if len(targetNames) != 1 || targetNames[0] != "orders-service" {
		t.Fatalf("targets = %v", targetNames)
	}
	identities := map[string]bool{}
	for _, identity := range output.DeployIdentities {
		identities[identity] = true
	}
	if !identities["orders-service"] {
		t.Fatalf("service identity missing: %v", output.DeployIdentities)
	}
}

func TestDeployChannelMalformedYAMLIsEmpty(t *testing.T) {
	output := DeployChannel{}.Parse("deployment.yaml", "kind: [broken\n  ::: not yaml {")
	if len(output.Evidence) != 0 || len(output.DeployIdentities) != 0 {
		t.Fatalf("a malformed manifest must yield empty output, got %+v", output)
	}
}

func TestDeployChannelSelect(t *testing.T) {
	tree := []FileEntry{
		{Path: "docker-compose.yml", IsDir: false},
		{Path: "compose.yaml", IsDir: false},
		{Path: "k8s/orders/deployment.yaml", IsDir: false},
		{Path: "helm/charts/orders/templates/service.yaml", IsDir: false},
		{Path: "helm/orders/values.yaml", IsDir: false}, // excluded
		{Path: "docs/service-notes.yml", IsDir: false},  // no keyword, no deploy dir
		{Path: "main.go", IsDir: false},
	}
	selected := DeployChannel{}.Select(tree)
	// The filename keyword match is deliberately loose (the Python original
	// shipped it): docs/service-notes.yml contains "service" and IS selected.
	want := []string{
		"compose.yaml", "docker-compose.yml", "docs/service-notes.yml",
		"helm/charts/orders/templates/service.yaml", "k8s/orders/deployment.yaml",
	}
	if fmt.Sprint(selected) != fmt.Sprint(want) {
		t.Fatalf("selected = %v, want %v (values.yaml and main.go stay out)", selected, want)
	}
}
