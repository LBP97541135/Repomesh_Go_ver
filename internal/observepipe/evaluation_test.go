package observepipe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscountFixtureBehaviorAndIsolation(t *testing.T) {
	seenTrials, seenEnvironments := map[string]bool{}, map[string]bool{}
	for _, tc := range []struct{ variant, verdict string }{{"baseline", "fail"}, {"candidate", "pass"}, {"assembly-mismatch", "unknown"}} {
		t.Run(tc.variant, func(t *testing.T) {
			r, err := RunDiscountFixture(context.Background(), tc.variant)
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict != tc.verdict || r.SubjectKind != "synthetic_fixed_product" {
				t.Fatalf("result: %+v", r)
			}
			if len(r.RequiredChecks) != 8 || len(r.Checks) != 8 {
				t.Fatalf("missing checks: %+v", r.Checks)
			}
			if seenTrials[r.TrialID] || seenEnvironments[r.Manifest.ExpectedPrice.EnvironmentID] {
				t.Fatal("fixture trial or environment reused")
			}
			seenTrials[r.TrialID], seenEnvironments[r.Manifest.ExpectedPrice.EnvironmentID] = true, true
			if !strings.HasPrefix(r.Manifest.ExpectedPrice.ArtifactDigest, "sha256:") || len(r.Manifest.ExpectedPrice.ArtifactDigest) != 71 {
				t.Fatal("missing actual executable digest")
			}
			if tc.variant == "baseline" {
				for _, id := range []string{"order.amount", "order.persisted_amount"} {
					check := testCheck(t, r, id)
					if check.Verdict != "fail" {
						t.Fatalf("%s did not fail: %+v", id, check)
					}
				}
				check := testCheck(t, r, "order.amount")
				if check.Expected != int64(8000) || check.Actual != int64(2000) {
					t.Fatalf("wrong numeric difference: %+v", check)
				}
			}
			if tc.variant == "assembly-mismatch" {
				if len(r.HTTPObservations) != 2 || testCheck(t, r, "order.identity").ReasonCode != "assembly_mismatch" {
					t.Fatal("mismatched combination must not run target business checks")
				}
			} else if len(r.HTTPObservations) != 5 {
				t.Fatalf("missing real HTTP observations: %d", len(r.HTTPObservations))
			}
			for _, endpoint := range []string{r.Manifest.PriceEndpoint, r.Manifest.OrderEndpoint} {
				client := &http.Client{Timeout: 100 * time.Millisecond}
				if resp, err := client.Get(endpoint + "/version"); err == nil {
					resp.Body.Close()
					t.Fatal("fixture server leaked after run")
				}
			}
		})
	}
}

func testCheck(t *testing.T, r TrialResult, id string) CheckResult {
	t.Helper()
	for _, check := range r.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing check %s", id)
	return CheckResult{}
}

func verifierTestManifest(t *testing.T, alter func(service string, w http.ResponseWriter, r *http.Request) bool) DiscountManifest {
	t.Helper()
	price := RuntimeIdentity{"price", "test-price/v1", "sha256:test-price", "test-environment"}
	order := RuntimeIdentity{"order", "test-order/v1", "sha256:test-order", "test-environment"}
	start := func(service string, identity RuntimeIdentity) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if alter != nil && alter(service, w, r) {
				return
			}
			switch r.URL.Path {
			case "/version":
				fixtureJSON(w, identity)
			case "/quote":
				fixtureJSON(w, map[string]any{"base_cents": 10000, "payable_basis_points": 8000, "amount_cents": 8000, "contract_version": "payable-ratio/v1"})
			case "/orders", "/orders/record-1":
				fixtureJSON(w, map[string]any{"order_id": "record-1", "amount_cents": 8000, "contract_version": "payable-ratio/v1", "price_identity": price})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		return server
	}
	priceServer, orderServer := start("price", price), start("order", order)
	return DiscountManifest{SchemaVersion: "discount-manifest/1", CaseID: "discount-semantics-001", CaseVersion: "1", VariantID: "test", CombinationID: "test-combination", SubjectKind: "verifier_contract_test", PriceEndpoint: priceServer.URL, OrderEndpoint: orderServer.URL, ExpectedPrice: price, ExpectedOrder: order, ContractVersion: "payable-ratio/v1", TestDataVersion: "discount-data/1", VerifierVersion: DiscountVerifierVersion, BaseCents: 10000, PayableBasisPoints: 8000, ExpectedCents: 8000, AssemblyResponsibility: "verifier", RequestTimeoutMillis: 500}
}

func TestVerifierRejectsSuccessClaimsAndMissingEvidence(t *testing.T) {
	for _, tc := range []struct{ name, body, verdict string }{
		{"empty_report", `{}`, "unknown"},
		{"broken_report", `{"amount_cents":`, "unknown"},
		{"zero_test_success", `{"status":"passed","test_count":0,"verdict":"pass"}`, "unknown"},
		{"known_failure_missing_contract", `{"order_id":"record-1","amount_cents":2000}`, "fail"},
		{"self_claimed_pass", `{"order_id":"record-1","amount_cents":2000,"contract_version":"payable-ratio/v1","price_identity":{"component":"price","revision":"test-price/v1","artifact_digest":"sha256:test-price","environment_id":"test-environment"},"verdict":"pass"}`, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
				if service == "order" && r.URL.Path == "/orders" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
					return true
				}
				return false
			})
			result, err := VerifyDiscount(context.Background(), manifest)
			if err != nil {
				t.Fatal(err)
			}
			if result.Verdict != tc.verdict {
				t.Fatalf("got %s; checks %+v", result.Verdict, result.Checks)
			}
		})
	}
}

func TestVerifierFailureSurvivesMissingIndependentCheck(t *testing.T) {
	manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
		if service == "price" && r.URL.Path == "/quote" {
			fixtureJSON(w, map[string]any{"base_cents": 10000, "payable_basis_points": 8000, "amount_cents": 2000, "contract_version": "payable-ratio/v1"})
			return true
		}
		if service == "order" && r.URL.Path == "/orders" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	result, err := VerifyDiscount(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "fail" || result.GradingStatus != "error" {
		t.Fatalf("known failure lost: %+v", result)
	}
	if check := testCheck(t, result, "order.amount"); check.Value != nil || check.Verdict != "unknown" {
		t.Fatalf("unknown got a score: %+v", check)
	}
}

func TestVerifierFailureSurvivesInvalidSiblingField(t *testing.T) {
	for _, tc := range []struct{ name, service, path, body, check string }{
		{"order_contract_after_amount", "order", "/orders", `{"order_id":"record-1","amount_cents":2000,"contract_version":42}`, "order.amount"},
		{"order_contract_before_amount", "order", "/orders", `{"contract_version":42,"order_id":"record-1","amount_cents":2000}`, "order.amount"},
		{"order_invalid_price_identity", "order", "/orders", `{"order_id":"record-1","amount_cents":2000,"price_identity":{"component":42}}`, "order.amount"},
		{"price_invalid_contract", "price", "/quote", `{"base_cents":10000,"payable_basis_points":8000,"amount_cents":2000,"contract_version":42}`, "price.amount"},
		{"price_invalid_base", "price", "/quote", `{"base_cents":"not-an-integer","payable_basis_points":8000,"amount_cents":2000,"contract_version":"payable-ratio/v1"}`, "price.amount"},
		{"price_wrong_base_invalid_contract", "price", "/quote", `{"base_cents":9000,"payable_basis_points":8000,"amount_cents":8000,"contract_version":42}`, "price.contract"},
		{"price_wrong_ratio_missing_base", "price", "/quote", `{"payable_basis_points":7000,"amount_cents":8000,"contract_version":"payable-ratio/v1"}`, "price.contract"},
		{"stored_invalid_contract", "order", "/orders/record-1", `{"order_id":"record-1","amount_cents":2000,"contract_version":42}`, "order.persisted_amount"},
		{"stored_wrong_id_missing_amount", "order", "/orders/record-1", `{"order_id":"wrong-record"}`, "order.persisted_amount"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
				if service == tc.service && r.URL.Path == tc.path {
					_, _ = w.Write([]byte(tc.body))
					return true
				}
				return false
			})
			result, err := VerifyDiscount(context.Background(), manifest)
			if err != nil {
				t.Fatal(err)
			}
			check := testCheck(t, result, tc.check)
			if result.Verdict != "fail" || check.Verdict != "fail" || check.Status != "scored" {
				t.Fatalf("valid amount failure lost to sibling error: verdict=%s check=%+v", result.Verdict, check)
			}
			if (tc.check == "price.amount" || tc.check == "order.amount") && check.Actual != int64(2000) {
				t.Fatalf("valid amount lost: %+v", check)
			}
		})
	}
}

func TestVerifierInvalidAmountIsUnknownWithoutCoercion(t *testing.T) {
	for _, amount := range []string{`"2000"`, `true`, `2000.5`, `2000.0`, `2e3`, `9223372036854775808`, `-9223372036854775809`, `null`, `[]`, `{}`} {
		t.Run(amount, func(t *testing.T) {
			manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
				if service == "order" && r.URL.Path == "/orders" {
					_, _ = w.Write([]byte(`{"order_id":"record-1","amount_cents":` + amount + `,"contract_version":42}`))
					return true
				}
				return false
			})
			result, err := VerifyDiscount(context.Background(), manifest)
			if err != nil {
				t.Fatal(err)
			}
			check := testCheck(t, result, "order.amount")
			if result.Verdict != "unknown" || check.Verdict != "unknown" || check.Value != nil || check.Actual != nil {
				t.Fatalf("invalid amount coerced to a score: verdict=%s check=%+v", result.Verdict, check)
			}
		})
	}
}

func TestAssemblyResponsibilityIsFrozenBeforeEvaluation(t *testing.T) {
	manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
		if service == "order" && r.URL.Path == "/version" {
			fixtureJSON(w, RuntimeIdentity{"order", "unexpected-build", "sha256:test-order", "test-environment"})
			return true
		}
		return false
	})
	for _, tc := range []struct{ responsibility, verdict string }{{"verifier", "unknown"}, {"subject", "fail"}} {
		manifest.AssemblyResponsibility = tc.responsibility
		result, err := VerifyDiscount(context.Background(), manifest)
		if err != nil {
			t.Fatal(err)
		}
		if result.Verdict != tc.verdict || len(result.HTTPObservations) != 2 {
			t.Fatalf("wrong attribution: %+v", result)
		}
	}
}

func TestVerifierTimeoutIsUnknownWithoutZeroScore(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
		if service == "order" && r.URL.Path == "/orders" {
			// The server need not detect a closed connection until it reads the
			// request body. Release it explicitly after the client timeout, so
			// cleanup cannot deadlock on this deliberately stalled handler.
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return true
		}
		return false
	})
	manifest.RequestTimeoutMillis = 20
	result, err := VerifyDiscount(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "unknown" || result.GradingStatus != "error" {
		t.Fatalf("timeout counted as success: %+v", result)
	}
	for _, check := range result.Checks {
		if check.Verdict == "unknown" && check.Value != nil {
			t.Fatal("missing score became zero")
		}
	}
	if len(result.HTTPObservations) != 4 || result.HTTPObservations[3].Error == "" {
		t.Fatal("timeout evidence missing")
	}
}

func TestVerifierDetectsPersistedRecordMismatch(t *testing.T) {
	manifest := verifierTestManifest(t, func(service string, w http.ResponseWriter, r *http.Request) bool {
		if service == "order" && strings.HasPrefix(r.URL.Path, "/orders/") {
			fixtureJSON(w, map[string]any{"order_id": "another-record", "amount_cents": 8000})
			return true
		}
		return false
	})
	result, err := VerifyDiscount(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "fail" || testCheck(t, result, "order.persisted_amount").ReasonCode != "persisted_order_mismatch" {
		t.Fatal("unrelated persisted order accepted")
	}
}

func TestAggregateChecksMissingAndConflictingResults(t *testing.T) {
	for _, tc := range []struct {
		name            string
		required        []string
		checks          []CheckResult
		verdict, status string
	}{
		{"no_tests", nil, nil, "unknown", "pending"},
		{"missing", []string{"a"}, nil, "unknown", "partial"},
		{"not_applicable_required", []string{"a"}, []CheckResult{{ID: "a", Status: "not_applicable", Verdict: "not_applicable"}}, "unknown", "partial"},
		{"fail_and_unknown", []string{"a", "b"}, []CheckResult{{ID: "a", Status: "scored", Verdict: "fail"}}, "fail", "partial"},
		{"conflicting_attempts", []string{"a"}, []CheckResult{{ID: "a", Status: "scored", Verdict: "pass"}, {ID: "a", Status: "unknown", Verdict: "unknown"}}, "unknown", "partial"},
		{"all_required_pass", []string{"a"}, []CheckResult{{ID: "a", Status: "scored", Verdict: "pass"}, {ID: "optional", Status: "error", Verdict: "unknown"}}, "pass", "complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, status := AggregateChecks(tc.required, tc.checks)
			if verdict != tc.verdict || status != tc.status {
				t.Fatalf("got %s/%s", verdict, status)
			}
		})
	}
}

func TestManifestRejectsUnfrozenOracleAndUnsupportedVersion(t *testing.T) {
	valid := verifierTestManifest(t, nil)
	for _, change := range []func(*DiscountManifest){
		func(m *DiscountManifest) { m.ExpectedCents = 2000 }, func(m *DiscountManifest) { m.CaseVersion = "latest" },
		func(m *DiscountManifest) { m.ExpectedOrder.EnvironmentID = "other" }, func(m *DiscountManifest) { m.AssemblyResponsibility = "" },
		func(m *DiscountManifest) { m.PriceEndpoint = "http://user:secret@localhost" },
	} {
		m := valid
		change(&m)
		if _, err := VerifyDiscount(context.Background(), m); err == nil {
			t.Fatalf("invalid manifest accepted: %+v", m)
		}
	}
	if _, err := RunDiscountFixture(context.Background(), "pretend-agent-success"); err == nil {
		t.Fatal("unknown variant accepted")
	}
	data, err := json.Marshal(valid)
	if err != nil || !json.Valid(data) {
		t.Fatal("manifest cannot be frozen")
	}
}
