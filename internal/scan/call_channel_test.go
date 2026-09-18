package scan

import (
	"fmt"
	"testing"
)

func TestParseCallDeclarationsFeignForms(t *testing.T) {
	content := `
import org.springframework.cloud.openfeign.FeignClient;

@FeignClient(name = "ts-order-service")
public interface OrderClient {}

@FeignClient(value = "ts-payment-service")
public interface PaymentClient {}

@FeignClient("ts-user-service")
public interface UserClient {}

@FeignClient(name = "config-service", url = "${config.url}")
public interface ConfigClient {}

@FeignClient(url = "http://legacy")
public interface LegacyClient {}
`
	// url-only clients name no service; everything else contributes its
	// declared service name.
	names := parseCallDeclarations(content)
	want := []string{"ts-order-service", "ts-payment-service", "ts-user-service", "config-service"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestParseCallDeclarationsDubbo(t *testing.T) {
	content := `
@DubboReference(interfaceClass = com.acme.api.WarehouseService.class)
private WarehouseService warehouse;

@DubboReference(interfaceName = "com.acme.api.BillingService")
private BillingService billing;

@Reference
private UnanchoredService mystery;
`
	// interfaceClass is a class literal (.class); interfaceName is a string.
	// A @Reference with neither declares nothing to anchor on.
	names := parseCallDeclarations(content)
	want := []string{"com.acme.api.WarehouseService", "com.acme.api.BillingService"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestParseCallDeclarationsGrpcThreeLanguages(t *testing.T) {
	content := `
this.ordersStub = OrdersServiceGrpc.newBlockingStub(channel);
handler = billing_pb2_grpc.BillingServiceStub(channel);
client = warehousepb.NewWarehouseServiceClient(conn);
this.unrelated = something.NewClient(other);
`
	// All three generated-code shapes match; identical names would collapse
	// through the case-insensitive dedupe, so three distinct services prove
	// all three extractors fired — and the ordinary NewClient call never
	// matched.
	names := parseCallDeclarations(content)
	want := []string{"OrdersService", "BillingService", "WarehouseService"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestParseCallDeclarationsDeduplicatesCaseInsensitively(t *testing.T) {
	content := `
@FeignClient(name = "Ts-Order-Service")
@FeignClient(name = "ts-order-service")
`
	names := parseCallDeclarations(content)
	if len(names) != 1 || names[0] != "Ts-Order-Service" {
		t.Fatalf("names = %v, want first occurrence kept once", names)
	}
}

func TestRuntimeCallChannelSelectAndParse(t *testing.T) {
	tree := []FileEntry{
		{Path: "src/main/java/com/demo/OrderClient.java", IsDir: false},
		{Path: "src/main/java/com/demo/Order.java", IsDir: false}, // no pattern match
		{Path: "src/main/java/com/demo/PaymentService.java", IsDir: false},
		{Path: "src/main/java/com/demo/deep/nested/UserConfig.java", IsDir: false},
	}
	selected := RuntimeCallChannel{}.Select(tree)
	if len(selected) != 3 || selected[0] != "src/main/java/com/demo/OrderClient.java" {
		t.Fatalf("selected = %v", selected)
	}

	content := "@FeignClient(name = \"ts-order-service\")\npublic interface OrderClient {}\n"
	output := RuntimeCallChannel{}.Parse("OrderClient.java", content)
	if len(output.Deps) != 1 || output.Deps[0] != "ts-order-service" {
		t.Fatalf("deps = %v", output.Deps)
	}
	if len(output.Evidence) != 1 ||
		output.Evidence[0].Mechanism != MechanismRuntimeCall ||
		output.Evidence[0].Confidence != ConfidenceConfirmed {
		t.Fatalf("evidence = %+v", output.Evidence)
	}
}
