//go:build !unix && !windows

package secrets

func readRootFile(string) ([]byte, error) {
	return nil, ErrConfiguration
}
