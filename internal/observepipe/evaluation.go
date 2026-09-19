package observepipe

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DiscountVerifierVersion = "discount-http/1.0"

// RuntimeIdentity is a service's observed build identity. A production adapter
// must obtain it from a trusted deployment source; an agent's claimed SHA is not
// deployment evidence. Fixture revisions are explicitly synthetic labels.
type RuntimeIdentity struct {
	Component      string `json:"component"`
	Revision       string `json:"revision"`
	ArtifactDigest string `json:"artifact_digest"`
	EnvironmentID  string `json:"environment_id"`
}

type DiscountManifest struct {
	SchemaVersion          string          `json:"schema_version"`
	CaseID                 string          `json:"case_id"`
	CaseVersion            string          `json:"case_version"`
	VariantID              string          `json:"variant_id"`
	CombinationID          string          `json:"combination_id"`
	SubjectKind            string          `json:"subject_kind"`
	PriceEndpoint          string          `json:"price_endpoint"`
	OrderEndpoint          string          `json:"order_endpoint"`
	ExpectedPrice          RuntimeIdentity `json:"expected_price"`
	ExpectedOrder          RuntimeIdentity `json:"expected_order"`
	ContractVersion        string          `json:"contract_version"`
	TestDataVersion        string          `json:"test_data_version"`
	VerifierVersion        string          `json:"verifier_version"`
	BaseCents              int64           `json:"base_cents"`
	PayableBasisPoints     int64           `json:"payable_basis_points"`
	ExpectedCents          int64           `json:"expected_cents"`
	AssemblyResponsibility string          `json:"assembly_responsibility"` // verifier or subject
	RequestTimeoutMillis   int64           `json:"request_timeout_ms"`
}

type CheckResult struct {
	ID           string   `json:"check_id"`
	Status       string   `json:"status"`
	Verdict      string   `json:"verdict"`
	Value        *float64 `json:"value"`
	Expected     any      `json:"expected"`
	Actual       any      `json:"actual"`
	ReasonCode   string   `json:"reason_code,omitempty"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// HTTPObservation preserves the actual response used by the independent
// verifier. Response content is evidence, never an instruction or a verdict.
type HTTPObservation struct {
	ID           string          `json:"observation_id"`
	Method       string          `json:"method"`
	URL          string          `json:"url"`
	Request      json.RawMessage `json:"request,omitempty"`
	StatusCode   int             `json:"status_code"`
	Response     json.RawMessage `json:"response,omitempty"`
	ResponseText string          `json:"response_text,omitempty"`
	Error        string          `json:"error,omitempty"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
}

type TrialResult struct {
	SchemaVersion    string            `json:"schema_version"`
	TrialID          string            `json:"trial_id"`
	CaseID           string            `json:"case_id"`
	CaseVersion      string            `json:"case_version"`
	VariantID        string            `json:"variant_id"`
	SubjectKind      string            `json:"subject_kind"`
	PublicTask       string            `json:"public_task"`
	Manifest         DiscountManifest  `json:"manifest"`
	GraderID         string            `json:"grader_id"`
	GraderVersion    string            `json:"grader_version"`
	GradingAttemptID string            `json:"grading_attempt_id"`
	RequiredChecks   []string          `json:"required_checks"`
	Checks           []CheckResult     `json:"checks"`
	HTTPObservations []HTTPObservation `json:"http_observations"`
	ExecutionStatus  string            `json:"execution_status"`
	GradingStatus    string            `json:"grading_status"`
	Verdict          string            `json:"verdict"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       time.Time         `json:"finished_at"`
	Limitations      []string          `json:"limitations"`
}

var discountRequiredChecks = []string{
	"price.identity", "order.identity", "price.contract", "price.amount",
	"order.contract", "order.price_identity", "order.amount", "order.persisted_amount",
}

// AggregateChecks uses the frozen list of required checks, not flags supplied by
// a grader. A known failure survives missing evidence; no tests never means pass.
// Multiple results for one check require an explicit selection before aggregation.
func AggregateChecks(required []string, checks []CheckResult) (verdict, gradingStatus string) {
	if len(required) == 0 {
		return "unknown", "pending"
	}
	byID := make(map[string][]CheckResult)
	for _, check := range checks {
		byID[check.ID] = append(byID[check.ID], check)
	}
	missing, failed, unhealthy := false, false, false
	seen := make(map[string]bool)
	for _, id := range required {
		if seen[id] || id == "" {
			missing, unhealthy = true, true
			continue
		}
		seen[id] = true
		results := byID[id]
		if len(results) != 1 {
			missing = true
		}
		for _, check := range results {
			if check.Status == "scored" && check.Verdict == "fail" {
				failed = true
			}
			if check.Status != "scored" || (check.Verdict != "pass" && check.Verdict != "fail") {
				missing = true
			}
			if check.Status == "error" {
				unhealthy = true
			}
		}
	}
	gradingStatus = "complete"
	if missing {
		gradingStatus = "partial"
	}
	if unhealthy {
		gradingStatus = "error"
	}
	if failed {
		return "fail", gradingStatus
	}
	if missing {
		return "unknown", gradingStatus
	}
	return "pass", gradingStatus
}

func checkKnown(id string, expected, actual any, pass bool, reason string, refs ...string) CheckResult {
	verdict, value := "fail", float64(0)
	if pass {
		verdict, value, reason = "pass", 1, ""
	}
	return CheckResult{ID: id, Status: "scored", Verdict: verdict, Value: &value, Expected: expected, Actual: actual, ReasonCode: reason, EvidenceRefs: refs}
}

func checkUnknown(id, reason string, refs ...string) CheckResult {
	return CheckResult{ID: id, Status: "unknown", Verdict: "unknown", ReasonCode: reason, EvidenceRefs: refs}
}

func validateDiscountManifest(m DiscountManifest) error {
	if m.SchemaVersion != "discount-manifest/1" || m.CaseID != "discount-semantics-001" || m.CaseVersion != "1" || m.VerifierVersion != DiscountVerifierVersion {
		return errors.New("unsupported discount manifest, case, or verifier version")
	}
	if m.VariantID == "" || m.CombinationID == "" || m.SubjectKind == "" || m.TestDataVersion == "" || m.ContractVersion == "" {
		return errors.New("discount manifest lacks frozen version identities")
	}
	// Version 1 has one public rule and input. Widening it requires a new case
	// version, rather than silently changing the oracle in a comparison.
	if m.BaseCents != 10000 || m.PayableBasisPoints != 8000 || m.ExpectedCents != 8000 {
		return errors.New("discount case version 1 requires 10000 cents, 8000 basis points, expected 8000 cents")
	}
	if m.AssemblyResponsibility != "verifier" && m.AssemblyResponsibility != "subject" {
		return errors.New("assembly_responsibility must be verifier or subject")
	}
	if m.RequestTimeoutMillis < 1 || m.RequestTimeoutMillis > 60000 {
		return errors.New("request_timeout_ms must be between 1 and 60000")
	}
	for _, endpoint := range []string{m.PriceEndpoint, m.OrderEndpoint} {
		u, err := url.Parse(endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("service endpoint must be an HTTP(S) origin without credentials, path, query, or fragment")
		}
	}
	for _, id := range []RuntimeIdentity{m.ExpectedPrice, m.ExpectedOrder} {
		if !identityComplete(id) {
			return errors.New("expected runtime identity is incomplete")
		}
	}
	if m.ExpectedPrice.Component != "price" || m.ExpectedOrder.Component != "order" || m.ExpectedPrice.EnvironmentID != m.ExpectedOrder.EnvironmentID {
		return errors.New("expected components must be price and order in one frozen environment")
	}
	return nil
}

func identityComplete(id RuntimeIdentity) bool {
	return id.Component != "" && id.Revision != "" && id.ArtifactDigest != "" && id.EnvironmentID != ""
}

type discountQuote struct {
	BaseCents          *int64 `json:"base_cents"`
	PayableBasisPoints *int64 `json:"payable_basis_points"`
	AmountCents        *int64 `json:"amount_cents"`
	ContractVersion    string `json:"contract_version"`
}

type discountOrder struct {
	OrderID         string          `json:"order_id"`
	AmountCents     *int64          `json:"amount_cents"`
	ContractVersion string          `json:"contract_version"`
	PriceIdentity   RuntimeIdentity `json:"price_identity"`
}

// Report fields are decoded independently: a malformed contract field cannot
// erase an observed wrong amount in the same otherwise valid JSON object.
// Invalid, missing, null, fractional, or overflowing integer values stay nil.
// Raw responses remain in HTTPObservation for diagnosing the invalid fields.
func reportField[T any](fields map[string]json.RawMessage, name string) *T {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value T
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func reportObject(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("report must be a JSON object")
	}
	return fields, nil
}

func (q *discountQuote) UnmarshalJSON(data []byte) error {
	fields, err := reportObject(data)
	if err != nil {
		return err
	}
	*q = discountQuote{
		BaseCents:          reportField[int64](fields, "base_cents"),
		PayableBasisPoints: reportField[int64](fields, "payable_basis_points"),
		AmountCents:        reportField[int64](fields, "amount_cents"),
	}
	if contract := reportField[string](fields, "contract_version"); contract != nil {
		q.ContractVersion = *contract
	}
	return nil
}

func (o *discountOrder) UnmarshalJSON(data []byte) error {
	fields, err := reportObject(data)
	if err != nil {
		return err
	}
	*o = discountOrder{AmountCents: reportField[int64](fields, "amount_cents")}
	if id := reportField[string](fields, "order_id"); id != nil {
		o.OrderID = *id
	}
	if contract := reportField[string](fields, "contract_version"); contract != nil {
		o.ContractVersion = *contract
	}
	if identity := reportField[RuntimeIdentity](fields, "price_identity"); identity != nil {
		o.PriceIdentity = *identity
	}
	return nil
}

// VerifyDiscount makes real HTTP requests and independently compares business
// values. Errors in configuration return an error; missing/invalid runtime
// evidence returns a recorded unknown, so it cannot be silently counted as pass.
func VerifyDiscount(ctx context.Context, manifest DiscountManifest) (TrialResult, error) {
	if err := validateDiscountManifest(manifest); err != nil {
		return TrialResult{}, err
	}
	id, err := evaluationID()
	if err != nil {
		return TrialResult{}, err
	}
	r := TrialResult{
		SchemaVersion: "repomesh-trial/1", TrialID: "trial-" + id,
		CaseID: manifest.CaseID, CaseVersion: manifest.CaseVersion, VariantID: manifest.VariantID,
		SubjectKind: manifest.SubjectKind, Manifest: manifest, GraderID: "delivery_acceptance",
		GraderVersion: DiscountVerifierVersion, GradingAttemptID: "grading-" + id,
		PublicTask:     "价格折扣表示应付比例：原价 10000 分，应付比例 8000/10000；报价、订单返回及持久化金额均为 8000 分。",
		RequiredChecks: append([]string(nil), discountRequiredChecks...), StartedAt: time.Now().UTC(),
		HTTPObservations: []HTTPObservation{}, Checks: []CheckResult{},
		Limitations: []string{"Service identity is observed through the configured deployment metadata endpoint; production deployment attestation is not implemented."},
	}
	if manifest.SubjectKind == "synthetic_fixed_product" {
		r.Limitations = append(r.Limitations, "Fixed localhost fixture exercise; no AgentTeams, DSH, model, repository selection, or generated candidate was executed.")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Duration(manifest.RequestTimeoutMillis) * time.Millisecond,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(name, method, endpoint string, input any, output any) (string, error) {
		observation, requestErr := observeJSONRequest(ctx, client, name, method, endpoint, input, output)
		r.HTTPObservations = append(r.HTTPObservations, observation)
		return "http:" + name, requestErr
	}
	addMissing := func(name, reason, ref string) {
		check := checkUnknown(name, reason, ref)
		check.Status = "error"
		r.Checks = append(r.Checks, check)
	}
	identityOK := true
	for _, service := range []struct {
		name, endpoint string
		expected       RuntimeIdentity
	}{{"price", manifest.PriceEndpoint, manifest.ExpectedPrice}, {"order", manifest.OrderEndpoint, manifest.ExpectedOrder}} {
		var actual RuntimeIdentity
		ref, requestErr := request(service.name+"-version", http.MethodGet, strings.TrimRight(service.endpoint, "/")+"/version", nil, &actual)
		check := checkKnown(service.name+".identity", service.expected, actual, actual == service.expected, "assembly_mismatch", ref)
		if requestErr != nil || !identityComplete(actual) {
			check = checkUnknown(service.name+".identity", "runtime_identity_unavailable", ref)
			check.Status = "error"
		} else if check.Verdict == "fail" && manifest.AssemblyResponsibility == "verifier" {
			check.Status, check.Verdict, check.Value = "error", "unknown", nil
		}
		if check.Verdict != "pass" {
			identityOK = false
		}
		r.Checks = append(r.Checks, check)
	}
	if identityOK {
		var quote discountQuote
		ref, requestErr := request("price-quote", http.MethodGet, strings.TrimRight(manifest.PriceEndpoint, "/")+"/quote?base_cents="+strconv.FormatInt(manifest.BaseCents, 10), nil, &quote)
		contractMismatch := (quote.BaseCents != nil && *quote.BaseCents != manifest.BaseCents) ||
			(quote.PayableBasisPoints != nil && *quote.PayableBasisPoints != manifest.PayableBasisPoints) ||
			(quote.ContractVersion != "" && quote.ContractVersion != manifest.ContractVersion)
		if requestErr != nil || (!contractMismatch && (quote.BaseCents == nil || quote.PayableBasisPoints == nil || quote.ContractVersion == "")) {
			addMissing("price.contract", "price_contract_report_missing_or_invalid", ref)
		} else {
			r.Checks = append(r.Checks,
				checkKnown("price.contract", map[string]any{"contract_version": manifest.ContractVersion, "base_cents": manifest.BaseCents, "payable_basis_points": manifest.PayableBasisPoints}, quote,
					!contractMismatch, "price_contract_mismatch", ref))
		}
		if requestErr != nil || quote.AmountCents == nil {
			addMissing("price.amount", "price_amount_missing_or_invalid", ref)
		} else {
			r.Checks = append(r.Checks, checkKnown("price.amount", manifest.ExpectedCents, *quote.AmountCents, *quote.AmountCents == manifest.ExpectedCents, "amount_mismatch", ref))
		}
		var order discountOrder
		ref, requestErr = request("order-create", http.MethodPost, strings.TrimRight(manifest.OrderEndpoint, "/")+"/orders", map[string]any{"base_cents": manifest.BaseCents}, &order)
		if requestErr != nil || order.ContractVersion == "" {
			addMissing("order.contract", "order_contract_missing_or_invalid", ref)
		} else {
			r.Checks = append(r.Checks, checkKnown("order.contract", manifest.ContractVersion, order.ContractVersion, order.ContractVersion == manifest.ContractVersion, "contract_not_synchronized", ref))
		}
		if requestErr != nil || !identityComplete(order.PriceIdentity) {
			addMissing("order.price_identity", "order_price_identity_missing_or_invalid", ref)
		} else {
			r.Checks = append(r.Checks, checkKnown("order.price_identity", manifest.ExpectedPrice, order.PriceIdentity, order.PriceIdentity == manifest.ExpectedPrice, "order_used_other_price_build", ref))
		}
		if requestErr != nil || order.AmountCents == nil {
			addMissing("order.amount", "order_amount_missing_or_invalid", ref)
		} else {
			r.Checks = append(r.Checks, checkKnown("order.amount", manifest.ExpectedCents, *order.AmountCents, *order.AmountCents == manifest.ExpectedCents, "amount_mismatch", ref))
		}
		if requestErr != nil || order.OrderID == "" {
			addMissing("order.persisted_amount", "order_identity_missing_or_invalid", ref)
		} else {
			var stored discountOrder
			ref, requestErr = request("order-read", http.MethodGet, strings.TrimRight(manifest.OrderEndpoint, "/")+"/orders/"+url.PathEscape(order.OrderID), nil, &stored)
			storedMismatch := (stored.OrderID != "" && stored.OrderID != order.OrderID) ||
				(stored.AmountCents != nil && *stored.AmountCents != manifest.ExpectedCents)
			if requestErr != nil || (!storedMismatch && (stored.AmountCents == nil || stored.OrderID == "")) {
				check := checkUnknown("order.persisted_amount", "persisted_order_report_missing_or_invalid", ref)
				check.Status = "error"
				r.Checks = append(r.Checks, check)
			} else {
				r.Checks = append(r.Checks, checkKnown("order.persisted_amount", map[string]any{"order_id": order.OrderID, "amount_cents": manifest.ExpectedCents}, stored,
					!storedMismatch, "persisted_order_mismatch", ref))
			}
		}
	} else {
		for _, name := range discountRequiredChecks[2:] {
			r.Checks = append(r.Checks, checkUnknown(name, "target_combination_not_verified"))
		}
	}
	r.Verdict, r.GradingStatus = AggregateChecks(r.RequiredChecks, r.Checks)
	r.ExecutionStatus, r.FinishedAt = "completed", time.Now().UTC()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.ExecutionStatus = "timed_out"
	} else if errors.Is(ctx.Err(), context.Canceled) {
		r.ExecutionStatus = "cancelled"
	}
	return r, nil
}

func observeJSONRequest(ctx context.Context, client *http.Client, id, method, endpoint string, input, output any) (o HTTPObservation, err error) {
	o = HTTPObservation{ID: id, Method: method, URL: endpoint, StartedAt: time.Now().UTC()}
	defer func() {
		o.FinishedAt = time.Now().UTC()
		if err != nil {
			o.Error = err.Error()
		}
	}()
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return o, err
		}
		o.Request = body
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return o, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return o, err
	}
	defer resp.Body.Close()
	o.StatusCode = resp.StatusCode
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return o, err
	}
	if len(data) > 1<<20 {
		return o, errors.New("response exceeds 1 MiB")
	}
	if json.Valid(data) {
		o.Response = data
	} else {
		o.ResponseText = string(data)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return o, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if err = json.Unmarshal(data, output); err != nil {
		return o, fmt.Errorf("invalid JSON response: %w", err)
	}
	return o, nil
}

func evaluationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RunDiscountFixture starts isolated price and order HTTP services, with order
// state written to a fresh temporary directory. It exercises the verifier only;
// it neither invokes an agent nor claims any generated-product improvement.
func RunDiscountFixture(ctx context.Context, variant string) (TrialResult, error) {
	if variant != "baseline" && variant != "candidate" && variant != "assembly-mismatch" {
		return TrialResult{}, errors.New("fixture variant must be baseline, candidate, or assembly-mismatch")
	}
	id, err := evaluationID()
	if err != nil {
		return TrialResult{}, err
	}
	dir, err := os.MkdirTemp("", "repomesh-discount-")
	if err != nil {
		return TrialResult{}, err
	}
	defer os.RemoveAll(dir)
	executable, err := os.Executable()
	if err != nil {
		return TrialResult{}, err
	}
	binary, err := os.Open(executable)
	if err != nil {
		return TrialResult{}, err
	}
	hash := sha256.New()
	_, hashErr := io.Copy(hash, binary)
	closeErr := binary.Close()
	if err := errors.Join(hashErr, closeErr); err != nil {
		return TrialResult{}, err
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	priceID := RuntimeIdentity{"price", "synthetic:price-payable/v1", digest, "fixture-" + id}
	orderID := RuntimeIdentity{"order", "synthetic:order-payable/v1", digest, priceID.EnvironmentID}
	if variant != "candidate" {
		orderID.Revision = "synthetic:order-legacy/v1"
	}
	priceMux := http.NewServeMux()
	priceMux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) { fixtureJSON(w, priceID) })
	priceMux.HandleFunc("GET /quote", func(w http.ResponseWriter, r *http.Request) {
		base, err := strconv.ParseInt(r.URL.Query().Get("base_cents"), 10, 64)
		if err != nil || base < 0 || base > 1_000_000_000 {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}
		ratio, amount := int64(8000), base*8000/10000
		fixtureJSON(w, discountQuote{&base, &ratio, &amount, "payable-ratio/v1"})
	})
	priceURL, stopPrice, err := startFixtureServer(priceMux)
	if err != nil {
		return TrialResult{}, err
	}
	defer stopPrice()
	fixtureTransport := http.DefaultTransport.(*http.Transport).Clone()
	defer fixtureTransport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: fixtureTransport}
	orderMux := http.NewServeMux()
	orderMux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) { fixtureJSON(w, orderID) })
	orderMux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			BaseCents *int64 `json:"base_cents"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input) != nil || input.BaseCents == nil || *input.BaseCents != 10000 {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}
		var quote discountQuote
		_, err := observeJSONRequest(r.Context(), client, "fixture-price", http.MethodGet, priceURL+"/quote?base_cents="+strconv.FormatInt(*input.BaseCents, 10), nil, &quote)
		if err != nil || quote.PayableBasisPoints == nil {
			http.Error(w, "price unavailable", http.StatusBadGateway)
			return
		}
		amount := *input.BaseCents * *quote.PayableBasisPoints / 10000
		contract := quote.ContractVersion
		if variant != "candidate" {
			amount = *input.BaseCents - amount
			contract = "reduction-ratio/v0"
		}
		orderKey, err := evaluationID()
		if err != nil {
			http.Error(w, "order identity unavailable", http.StatusInternalServerError)
			return
		}
		order := discountOrder{OrderID: orderKey, AmountCents: &amount, ContractVersion: contract, PriceIdentity: priceID}
		data, err := json.Marshal(order)
		if err != nil {
			http.Error(w, "encode order", http.StatusInternalServerError)
			return
		}
		if os.WriteFile(filepath.Join(dir, orderKey+".json"), data, 0600) != nil {
			http.Error(w, "persist order", http.StatusInternalServerError)
			return
		}
		fixtureJSON(w, order)
	})
	orderMux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("id")
		if len(key) != 32 || strings.ContainsAny(key, "/\\.") {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, key+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	orderURL, stopOrder, err := startFixtureServer(orderMux)
	if err != nil {
		return TrialResult{}, err
	}
	defer stopOrder()
	expectedOrder := orderID
	if variant == "assembly-mismatch" {
		expectedOrder.Revision = "synthetic:order-payable/v1"
	}
	manifest := DiscountManifest{
		SchemaVersion: "discount-manifest/1", CaseID: "discount-semantics-001", CaseVersion: "1", VariantID: variant,
		CombinationID: "synthetic-combination-" + id, SubjectKind: "synthetic_fixed_product",
		PriceEndpoint: priceURL, OrderEndpoint: orderURL, ExpectedPrice: priceID, ExpectedOrder: expectedOrder,
		ContractVersion: "payable-ratio/v1", TestDataVersion: "discount-data/1", VerifierVersion: DiscountVerifierVersion,
		BaseCents: 10000, PayableBasisPoints: 8000, ExpectedCents: 8000, AssemblyResponsibility: "verifier", RequestTimeoutMillis: 2000,
	}
	return VerifyDiscount(ctx, manifest)
}

func fixtureJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func startFixtureServer(handler http.Handler) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	var once sync.Once
	stop := func() { once.Do(func() { _ = server.Close(); <-done }) }
	return "http://" + listener.Addr().String(), stop, nil
}
