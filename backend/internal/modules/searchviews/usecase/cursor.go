package usecase

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strconv"
)

type Cursor struct {
	Workspace   uuid.UUID `json:"workspace"`
	Actor       uuid.UUID `json:"actor"`
	Session     uuid.UUID `json:"session"`
	Resource    string    `json:"resource"`
	Fingerprint string    `json:"fingerprint"`
	Ordinal     string    `json:"ordinal"`
}

func EncodeCursor(v Cursor) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
func DecodeCursor(s string) (Cursor, error) {
	var v Cursor
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil || len(s) > 2048 {
		return v, errors.New("invalid cursor")
	}
	e = json.Unmarshal(b, &v)
	n, err := strconv.ParseInt(v.Ordinal, 10, 64)
	if e != nil || err != nil || n < 1 || strconv.FormatInt(n, 10) != v.Ordinal || EncodeCursor(v) != s {
		return Cursor{}, errors.New("invalid cursor")
	}
	return v, nil
}

type viewCursor struct {
	Workspace, Actor, Anchor uuid.UUID
	Resource                 string
	Deleted                  bool
}

func ViewCursor(w, a uuid.UUID, in ViewListInput, id uuid.UUID) string {
	b, _ := json.Marshal(viewCursor{w, a, id, in.Resource, in.IncludeDeleted})
	return base64.RawURLEncoding.EncodeToString(b)
}
func ViewAnchor(w, a uuid.UUID, in ViewListInput) (uuid.UUID, error) {
	if in.After == "" {
		return uuid.Nil, nil
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(in.After)
	var v viewCursor
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	if e != nil || v.Workspace != w || v.Actor != a || v.Resource != in.Resource || v.Deleted != in.IncludeDeleted || v.Anchor == uuid.Nil || ViewCursor(w, a, in, v.Anchor) != in.After {
		return uuid.Nil, errors.New("invalid cursor")
	}
	return v.Anchor, nil
}
