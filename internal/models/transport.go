package models

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// SingleRequestTransport sends exactly one HTTPS chat-completions request per
// dispatch: no retry, no redirects, no environment proxy. The fixed prompt and
// the stored output/time limits travel with the dispatch; TLS is pinned to the
// resolved IP so a hostname can never swap mid-flight.
type SingleRequestTransport struct {
	protocolVersion string
}

func NewSingleRequestTransport(protocolVersion string) (*SingleRequestTransport, error) {
	if protocolVersion == "" || len(protocolVersion) > 64 {
		return nil, failure(2, "TRANSPORT_PROTOCOL_INVALID")
	}
	return &SingleRequestTransport{protocolVersion: protocolVersion}, nil
}

const fixedTestPrompt = "Reply with the single word: pong."

func (t *SingleRequestTransport) ProtocolVersion() string { return t.protocolVersion }

// sendOnce performs the single send and returns the observation. It never
// retries: a transport-level failure yields code "transport_failure" so the
// runner reconciles the reservation instead of recording a model result.
func (t *SingleRequestTransport) sendOnce(ctx context.Context, dispatch *AuthorizedDispatch) DispatchObservation {
	started := time.Now().UTC()
	observation := DispatchObservation{
		EvidenceID:          newID(),
		ActorID:             dispatch.permit.actorID,
		TestID:              dispatch.permit.testID,
		ExternalOperationID: dispatch.permit.externalOperationID,
		StartedAt:           &started,
		Generation:          1,
	}
	body, code, latency, usage, err := t.doSend(ctx, dispatch)
	observation.ObservedAt = time.Now().UTC()
	observation.LatencyMS = ptrInt64(latency.Milliseconds())
	observation.Usage = usage
	if err != nil {
		observation.Code = code
		if body != "" {
			observation.Code = code
		}
		return observation
	}
	observation.Code = code
	_ = body
	return observation
}

func ptrInt64(v int64) *int64 { return &v }

// approveURL verifies the request URL against the stored egress whitelist and
// resolves the host to IPs that are neither private nor loopback unless the
// policy explicitly allows private addresses.
func approveURL(rawURL string, policy TestPolicy) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, failure(409, "EGRESS_URL_REJECTED")
	}
	if len(policy.ApprovedBaseURLs) > 0 {
		ok := false
		for _, base := range policy.ApprovedBaseURLs {
			if parsed.Host == base || strings_hasPrefix(parsed.Host, base) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, failure(409, "EGRESS_URL_REJECTED")
		}
	}
	return parsed, nil
}

func strings_hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// dialPinned resolves the host and dials the first dialable address, skipping
// private/loopback ranges unless the policy allows them.
func dialPinned(ctx context.Context, host string, port string, allowPrivate bool) (net.Conn, error) {
	resolver := &net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, failure(409, "EGRESS_HOST_UNRESOLVED")
	}
	var lastErr error
	for _, ip := range ips {
		addr := ip.IP
		if !allowPrivate && (addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified()) {
			continue
		}
		conn, dialErr := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
		if dialErr != nil {
			lastErr = dialErr
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, failure(409, "EGRESS_DIAL_FAILED")
	}
	return nil, failure(409, "EGRESS_ADDRESS_REJECTED")
}

func (t *SingleRequestTransport) doSend(ctx context.Context, dispatch *AuthorizedDispatch) (string, string, time.Duration, *TokenUsage, error) {
	start := time.Now()
	model := dispatch.model.Models[0]
	target, err := approveURL(dispatch.model.BaseURL, dispatch.policy)
	if err != nil {
		return "", "egress_rejected", 0, nil, err
	}
	port := target.Port()
	if port == "" {
		port = "443"
	}
	conn, err := dialPinned(ctx, target.Hostname(), port, dispatch.policy.AllowPrivateAddr)
	if err != nil {
		return "", "transport_failure", 0, nil, err
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: target.Hostname(), MinVersion: tls.VersionTLS12})
	if err = tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return "", "transport_failure", 0, nil, err
	}
	defer tlsConn.Close()

	requestTimeout := time.Duration(dispatch.policy.Limits.ModelRequestSeconds) * time.Second
	sendCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"model": model.ModelID,
		"messages": []map[string]string{
			{"role": "user", "content": fixedTestPrompt},
		},
		"max_tokens": dispatch.maxOutput,
	})
	if err != nil {
		return "", "transport_failure", 0, nil, err
	}
	endpoint := *target
	endpoint.Path = endpoint.Path + "/chat/completions"
	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return "", "transport_failure", 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(dispatch.key))
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return tlsConn, nil
		}, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: requestTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "transport_failure", 0, nil, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 1<<20)
	raw, err := io.ReadAll(limited)
	latency := time.Since(start)
	if err != nil {
		return "", "transport_failure", latency, nil, err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err = json.Unmarshal(raw, &parsed); err != nil {
		return "", "provider_invalid_response", latency, nil, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return string(raw), "rate_limited", latency, nil, nil
	}
	if resp.StatusCode >= 500 {
		return string(raw), "provider_unavailable", latency, nil, nil
	}
	if resp.StatusCode >= 400 || parsed.Error != nil {
		return string(raw), "provider_rejected", latency, nil, nil
	}
	if len(parsed.Choices) == 0 {
		return "", "provider_invalid_response", latency, nil, nil
	}
	usage := &TokenUsage{InputTokens: parsed.Usage.PromptTokens, OutputTokens: parsed.Usage.CompletionTokens}
	return parsed.Choices[0].Message.Content, "success", latency, usage, nil
}
