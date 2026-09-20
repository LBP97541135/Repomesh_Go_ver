package deliverymanifest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func newID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("deliverymanifest: id generation failed: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	sum := hex.EncodeToString(buffer)
	return fmt.Sprintf("%s-%s-%s-%s-%s", sum[0:8], sum[8:12], sum[12:16], sum[16:20], sum[20:32]), nil
}

// hashOf 是这次建清单的请求指纹：同键不同含义时能看出来（幂等校验用）。
func hashOf(command BuildCommand) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		command.ProjectID, command.IssueID, command.PlanID,
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func joinStrings(items []string, sep string) string { return strings.Join(items, sep) }
