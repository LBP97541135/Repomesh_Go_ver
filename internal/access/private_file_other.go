//go:build !linux && !windows

package access

import "errors"

func readPrivateFile(string) ([]byte, error) {
	return nil, errors.New("authentication credential file validation is currently supported on Linux only")
}
