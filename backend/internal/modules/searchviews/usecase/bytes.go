package usecase

import (
	"bytes"
	"encoding/json"
)

// domainBytes measures JSON.stringify-compatible UTF-8 bytes of the accepted
// typed domain. Counting never changes input values or persisted text.
func domainBytes(value any) (int, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if e := encoder.Encode(value); e != nil {
		return 0, e
	}
	encoded := buffer.Bytes()
	size := len(encoded) - 1
	inside := false
	for i := 0; i < len(encoded); {
		if encoded[i] == '"' {
			inside = !inside
			i++
			continue
		}
		if inside && encoded[i] == '\\' {
			if i+6 <= len(encoded) && (string(encoded[i:i+6]) == `\u2028` || string(encoded[i:i+6]) == `\u2029`) {
				size -= 3
				i += 6
			} else {
				i += 2
			}
			continue
		}
		// Every accepted domain number is an int32. -0 is a valid integer spelling
		// which JSON.stringify emits as 0. Do not alter the text string "-0".
		if !inside && encoded[i] == '-' && i+1 < len(encoded) && encoded[i+1] == '0' && (i+2 == len(encoded) || bytes.ContainsRune([]byte(",]} \n"), rune(encoded[i+2]))) {
			size--
			i += 2
			continue
		}
		i++
	}
	return size, nil
}
