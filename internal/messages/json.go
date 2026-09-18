package messages

import (
	"bytes"
	"encoding/json"
)

func decodeSubmissionReceipt(data []byte) (SubmissionReceipt, error) {
	var receipt SubmissionReceipt
	err := json.Unmarshal(data, &receipt)
	return receipt, err
}

// hasDuplicateKeys reports whether the document repeats an object key.
func hasDuplicateKeys(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var walk func() bool
	walk = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return false
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return false
				}
				key, ok := keyToken.(string)
				if !ok {
					return false
				}
				if seen[key] {
					return true
				}
				seen[key] = true
				if walk() {
					return true
				}
			}
			_, err := decoder.Token()
			return err != nil
		case '[':
			for decoder.More() {
				if walk() {
					return true
				}
			}
			_, err := decoder.Token()
			return err != nil
		}
		return false
	}
	return walk()
}
