package usecase

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
)

type Cursor struct {
	Workspace       uuid.UUID `json:"w"`
	Actor           uuid.UUID `json:"a"`
	Session         uuid.UUID `json:"s"`
	Fingerprint     string    `json:"f"`
	Ordinal         string    `json:"o"`
	IncludeInactive bool      `json:"i"`
}

func EncodeCursor(v Cursor) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
func DecodeCursor(v string) (Cursor, error) {
	var out Cursor
	b, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b, &out)
	if e == nil && EncodeCursor(out) != v {
		e = fmt.Errorf("noncanonical cursor")
	}
	return out, e
}
func EncodeFavoriteCursor(w, a uuid.UUID, inactive bool, id uuid.UUID) string {
	return EncodeCursor(Cursor{Workspace: w, Actor: a, Session: id, IncludeInactive: inactive})
}
