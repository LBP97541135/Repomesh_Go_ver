package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func BrokerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("TYPESAFE_BROKER_URL_INVALID")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", errors.New("TYPESAFE_BROKER_URL_INVALID")
	}
	return strings.TrimRight(u.String(), "/") + "/api/typesafe/evaluations", nil
}

// Invoke is the Agent-side helper. It has only a run credential; no vault,
// database connection, or provider API key is needed in this process.
func Invoke(ctx context.Context, broker, token string, input io.Reader, output io.Writer) error {
	endpoint, err := BrokerURL(broker)
	if err != nil {
		return err
	}
	if len(token) != 43 {
		return errors.New("TYPESAFE_TOOL_UNAVAILABLE")
	}
	raw, err := io.ReadAll(io.LimitReader(input, MaxBody+1))
	if err != nil || len(raw) > MaxBody {
		return errors.New("TYPESAFE_INVALID_INPUT")
	}
	var in Input
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("TYPESAFE_INVALID_INPUT")
	}
	in, err = in.normalized()
	if err != nil {
		return err
	}
	raw, _ = json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("TYPESAFE_BROKER_URL_INVALID")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: CallTimeout + 5*time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("TYPESAFE_BROKER_UNREACHABLE: retry with the same requestId and content")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 128<<10))
	if err != nil {
		return errors.New("TYPESAFE_BROKER_UNREACHABLE")
	}
	if res.StatusCode != http.StatusOK {
		var fault struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &fault) == nil && strings.HasPrefix(fault.Code, "TYPESAFE_") && safeID.MatchString(fault.Code) {
			return errors.New(fault.Code)
		}
		return errors.New("TYPESAFE_BROKER_REJECTED")
	}
	var result Evaluation
	if json.Unmarshal(body, &result) != nil || result.ID == "" {
		return errors.New("TYPESAFE_INVALID_RESPONSE")
	}
	return json.NewEncoder(output).Encode(result)
}
