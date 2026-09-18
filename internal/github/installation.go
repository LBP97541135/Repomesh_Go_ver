package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// AppInstallationObservation retains the validated installation identity and
// permission set that AppCapability previously discarded. Construction only
// happens in this adapter; callers never assemble one from raw parts.
type AppInstallationObservation struct {
	installationID     int64
	appID              int64
	permissionRevision string
	capability         Capability
	observedAt         time.Time
}

func (o AppInstallationObservation) InstallationID() int64      { return o.installationID }
func (o AppInstallationObservation) PermissionRevision() string { return o.permissionRevision }
func (o AppInstallationObservation) Capability() Capability     { return o.capability }
func (o AppInstallationObservation) ObservedAt() time.Time      { return o.observedAt }

// ObservedInstallation assembles a validated observation from raw observation
// facts. The permission revision is computed here, never accepted from the
// caller, so test doubles and the adapter share one fingerprint definition.
func ObservedInstallation(appID, installationID int64, suspended bool, permissions map[string]string, capability Capability, observedAt time.Time) AppInstallationObservation {
	return AppInstallationObservation{
		installationID:     installationID,
		appID:              appID,
		permissionRevision: installationPermissionRevision(appID, installationID, suspended, permissions),
		capability:         capability,
		observedAt:         observedAt,
	}
}

// ObserveAppInstallation reads the repository's App installation through the
// validated /installation endpoint and returns the full local observation.
func (c *Client) ObserveAppInstallation(ctx context.Context, owner, name string) (AppInstallationObservation, error) {
	if !pathSegment(owner) || !pathSegment(name) {
		return AppInstallationObservation{}, &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	token, err := c.appToken(ctx)
	if err != nil {
		return AppInstallationObservation{}, err
	}
	target := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/installation"
	payload, _, status, err := c.request(ctx, http.MethodGet, target, token, nil)
	observed := time.Now().UTC()
	if err != nil {
		var failure *Error
		if status == http.StatusNotFound && errors.As(err, &failure) && failure.Kind == "denied" {
			return AppInstallationObservation{
				capability: Capability{Status: "denied", ReasonCodes: []string{"APP_INSTALLATION_MISSING"}, ObservedAt: &observed},
				observedAt: observed,
			}, nil
		}
		return AppInstallationObservation{}, err
	}
	var response struct {
		ID          int64             `json:"id"`
		AppID       int64             `json:"app_id"`
		SuspendedAt json.RawMessage   `json:"suspended_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := decode(payload, &response); err != nil {
		return AppInstallationObservation{}, err
	}
	if response.ID <= 0 || strconv.FormatInt(response.AppID, 10) != c.config.AppID || len(response.SuspendedAt) == 0 || response.Permissions == nil {
		return AppInstallationObservation{}, &Error{Kind: "unavailable"}
	}
	capability := Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observed}
	suspended := false
	if !bytes.Equal(response.SuspendedAt, []byte("null")) {
		var suspendedAt time.Time
		if json.Unmarshal(response.SuspendedAt, &suspendedAt) != nil || suspendedAt.IsZero() {
			return AppInstallationObservation{}, &Error{Kind: "unavailable"}
		}
		capability.Status = "denied"
		capability.ReasonCodes = append(capability.ReasonCodes, "APP_INSTALLATION_SUSPENDED")
		suspended = true
	}
	if response.Permissions["contents"] != "write" || response.Permissions["pull_requests"] != "write" || response.Permissions["metadata"] != "read" {
		capability.Status = "denied"
		capability.ReasonCodes = append(capability.ReasonCodes, "APP_PERMISSION_MISSING")
	}
	appID, err := strconv.ParseInt(c.config.AppID, 10, 64)
	if err != nil || appID <= 0 {
		return AppInstallationObservation{}, &Error{Kind: "unavailable"}
	}
	return AppInstallationObservation{
		installationID:     response.ID,
		appID:              appID,
		permissionRevision: installationPermissionRevision(appID, response.ID, suspended, response.Permissions),
		capability:         capability,
		observedAt:         observed,
	}, nil
}

// installationPermissionRevision is a versioned local fingerprint of the
// observed authorization state: length-delimited appID/installationID/suspended
// plus the sorted complete permission key/value set. It is an observation
// fingerprint, not a GitHub CAS revision and not proof of unchanged remote
// authorization until commit.
func installationPermissionRevision(appID, installationID int64, suspended bool, permissions map[string]string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte("repomesh-installation-v1\x00"))
	writeField(digest, strconv.FormatInt(appID, 10))
	writeField(digest, strconv.FormatInt(installationID, 10))
	if suspended {
		writeField(digest, "1")
	} else {
		writeField(digest, "0")
	}
	keys := make([]string, 0, len(permissions))
	for key := range permissions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeField(digest, key)
		writeField(digest, permissions[key])
	}
	return "v1:" + hex.EncodeToString(digest.Sum(nil))
}

func writeField(digest io.Writer, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}
