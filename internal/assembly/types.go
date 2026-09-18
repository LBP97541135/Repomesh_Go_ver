package assembly

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/agentteams"
)

// Service provisions the three-level role topology (M4).
type Service struct {
	pool        *pgxpool.Pool
	at          *agentteams.Client
	provisioner TeamProvisioner
}

// New wires the assembly service. The AT client is optional: nil means
// topology rows are recorded locally and the remote provisioning stays
// genuinely absent (never faked).
func New(pool *pgxpool.Pool, at *agentteams.Client, provisioner TeamProvisioner) *Service {
	return &Service{pool: pool, at: at, provisioner: provisioner}
}

func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("assembly: id generation failed: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}

// newAgentUUID mints a UUID-v4-shaped identifier; public.agents.id is uuid.
func newAgentUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("assembly: id generation failed: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	hexSum := hex.EncodeToString(buffer)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[12:16], hexSum[16:20], hexSum[20:32]), nil
}

var _ = time.Now
