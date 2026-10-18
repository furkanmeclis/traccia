package usecase

import (
	"bytes"
	"encoding/json"
)

// contentBytes measures the same JSON representation as the shared browser
// contract. Encoding choices do not count HTML escapes as additional content.
func contentBytes(content []Node) (int, error) {
	raw, e := json.Marshal(content)
	if e != nil {
		return 0, e
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e = decoder.Decode(&value); e != nil {
		return 0, e
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if e = encoder.Encode(value); e != nil {
		return 0, e
	}
	encoded := buffer.Bytes()
	size := len(encoded) - 1
	for i := 0; i < len(encoded); {
		if encoded[i] != '\\' {
			i++
			continue
		}
		if i+6 <= len(encoded) && (string(encoded[i:i+6]) == `\u2028` || string(encoded[i:i+6]) == `\u2029`) {
			size -= 3
			i += 6
		} else {
			i += 2
		}
	}
	return size, nil
}
