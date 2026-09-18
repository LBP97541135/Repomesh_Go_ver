package scan

import (
	"regexp"
	"sort"
	"strings"
)

// RuntimeCallChannel is mechanism ② RUNTIME_CALL: framework-declared
// service calls (Feign/Dubbo/gRPC). Only the *declared* name is evidence —
// a string literal that merely looks like a service name is not, which is
// exactly what the deleted first-generation regex guessing did.
type RuntimeCallChannel struct{}

// NewRuntimeCallChannel returns the channel ready for RegisterChannel.
func NewRuntimeCallChannel() RuntimeCallChannel { return RuntimeCallChannel{} }

// Name is the channel id and the dedupe namespace for its evidence.
func (RuntimeCallChannel) Name() string { return "RUNTIME_CALL" }

// javaScanPatterns: Feign clients, Dubbo consumers and gRPC stubs cluster
// in these filename shapes.
var javaScanPatterns = []string{
	"ServiceImpl.java", "Service.java", "Controller.java",
	"Client.java", "Config.java", "Grpc.java",
}

// runtimeCallMaxFiles bounds upstream fetches per repository.
const runtimeCallMaxFiles = 15

// Select picks Java files whose basename carries one of the declaration
// shapes, sorted for determinism.
func (RuntimeCallChannel) Select(tree []FileEntry) []string {
	var candidates []string
	for _, entry := range tree {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".java") {
			continue
		}
		basename := basename(entry.Path)
		for _, pattern := range javaScanPatterns {
			if strings.Contains(basename, pattern) {
				candidates = append(candidates, entry.Path)
				break
			}
		}
	}
	sort.Strings(candidates)
	if len(candidates) > runtimeCallMaxFiles {
		candidates = candidates[:runtimeCallMaxFiles]
	}
	return candidates
}

var (
	feignAnnotation = regexp.MustCompile(`@FeignClient\s*\(\s*([^)]*)\)`)
	feignBareString = regexp.MustCompile(`^\s*["']([^"']+)["']`)

	dubboReference = regexp.MustCompile(`@(?:DubboReference|Reference)\s*\(\s*([^)]*)\)`)
	dubboClass     = regexp.MustCompile(`\binterfaceClass\s*=\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)\s*\.class`)
	dubboName      = regexp.MustCompile(`\binterfaceName\s*=\s*["']([^"']+)["']`)

	grpcJavaStub   = regexp.MustCompile(`([A-Za-z_]\w*)Grpc\.new[A-Za-z_]*Stub\s*\(`)
	grpcPythonStub = regexp.MustCompile(`[A-Za-z_]\w*_pb2_grpc\.([A-Za-z_]\w*)Stub\s*\(`)
	grpcGoStub     = regexp.MustCompile(`\.New([A-Za-z_]\w*)Client\s*\(`)
)

// annotationAttr extracts one annotation attribute's string literal.
func annotationAttr(body, attr string) string {
	match := regexp.MustCompile(`\b` + regexp.QuoteMeta(attr) + `\s*=\s*["']([^"']+)["']`).FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return match[1]
}

// feignTargets: the service name is the `name` attribute, falling back to
// `value` (the single-argument form), then to the bare-string form. A
// client configured only with `url=` names no service and contributes
// nothing.
func feignTargets(content string) []string {
	var targets []string
	for _, match := range feignAnnotation.FindAllStringSubmatch(content, -1) {
		body := match[1]
		name := annotationAttr(body, "name")
		if name == "" {
			name = annotationAttr(body, "value")
		}
		if name == "" {
			if bare := feignBareString.FindStringSubmatch(body); bare != nil {
				name = bare[1]
			}
		}
		if name != "" {
			targets = append(targets, name)
		}
	}
	return targets
}

// dubboTargets: the consumer declares the called interface as
// interfaceClass (a class literal) or interfaceName (a string). A
// @Reference with neither declares nothing to anchor on.
func dubboTargets(content string) []string {
	var targets []string
	for _, match := range dubboReference.FindAllStringSubmatch(content, -1) {
		body := match[1]
		name := ""
		if class := dubboClass.FindStringSubmatch(body); class != nil {
			name = class[1]
		} else if named := dubboName.FindStringSubmatch(body); named != nil {
			name = named[1]
		}
		if name != "" {
			targets = append(targets, name)
		}
	}
	return targets
}

// grpcTargets: generated stub constructors name the target twice
// (OrderServiceGrpc / order_pb2_grpc.OrderServiceStub /
// orderpb.NewOrderServiceClient); the service name is the OrderService
// part, anchored to the generated-code shape so ordinary method calls
// never match.
func grpcTargets(content string) []string {
	var targets []string
	for _, match := range grpcJavaStub.FindAllStringSubmatch(content, -1) {
		targets = append(targets, match[1])
	}
	for _, match := range grpcPythonStub.FindAllStringSubmatch(content, -1) {
		targets = append(targets, match[1])
	}
	for _, match := range grpcGoStub.FindAllStringSubmatch(content, -1) {
		targets = append(targets, match[1])
	}
	return targets
}

// parseCallDeclarations extracts declared call targets from one source
// file, deduplicated case-insensitively (first occurrence wins). Never
// fails: a file declaring nothing yields empty output.
func parseCallDeclarations(content string) []string {
	var names []string
	seen := map[string]bool{}
	appendTarget := func(name string) {
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		names = append(names, name)
	}
	for _, name := range feignTargets(content) {
		appendTarget(name)
	}
	for _, name := range dubboTargets(content) {
		appendTarget(name)
	}
	for _, name := range grpcTargets(content) {
		appendTarget(name)
	}
	return names
}

// Parse implements EvidenceChannel: every declared target lands in deps
// (keyword discovery) and as confirmed RUNTIME_CALL evidence (the graph).
func (RuntimeCallChannel) Parse(filename, content string) ChannelOutput {
	output := ChannelOutput{}
	for _, name := range parseCallDeclarations(content) {
		output.Deps = append(output.Deps, name)
		output.Evidence = append(output.Evidence, DepEvidence{
			Name:       name,
			Mechanism:  MechanismRuntimeCall,
			Confidence: ConfidenceConfirmed,
		})
	}
	return output
}
