package usecase

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"mime"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func field(name, code string) []operations.FieldError {
	return []operations.FieldError{{Field: name, Code: code}}
}
func ID(id uuid.UUID, name string) []operations.FieldError {
	if id == uuid.Nil {
		return field(name, "invalid")
	}
	return nil
}
func validateVersion(version int64) []operations.FieldError {
	if version < 1 {
		return field("expected_version", "invalid")
	}
	return nil
}
func validSHA(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, e := hex.DecodeString(value)
	return e == nil
}
func DecodeChunk(in ChunkInput) ([]byte, []operations.FieldError) {
	if len(in.DataBase64) > 4*((int(MaxChunkBytes)+2)/3) {
		return nil, field("data_base64", "bytes_limit")
	}
	data, e := base64.StdEncoding.Strict().DecodeString(in.DataBase64)
	if e != nil || base64.StdEncoding.EncodeToString(data) != in.DataBase64 {
		return nil, field("data_base64", "invalid_base64")
	}
	if len(data) < 1 || len(data) > int(MaxChunkBytes) {
		return nil, field("data_base64", "bytes_limit")
	}
	sum := sha256.Sum256(data)
	if !validSHA(in.ChunkSHA256) || hex.EncodeToString(sum[:]) != in.ChunkSHA256 {
		return nil, field("chunk_sha256", "mismatch")
	}
	return data, nil
}
func validateCreate(in CreateInput) []operations.FieldError {
	if e := ID(in.DraftID, "draft_id"); len(e) > 0 {
		return e
	}
	if strings.ContainsRune(in.Filename, 0) || !utf8.ValidString(in.Filename) || utf8.RuneCountInString(in.Filename) < 1 || utf8.RuneCountInString(in.Filename) > 255 {
		return field("filename", "invalid")
	}
	if strings.ContainsRune(in.ContentType, 0) || !utf8.ValidString(in.ContentType) || utf8.RuneCountInString(in.ContentType) > 255 {
		return field("content_type", "invalid")
	}
	if in.SizeBytes < 0 || in.SizeBytes > MaxFileBytes {
		return field("size_bytes", "bytes_limit")
	}
	if !validSHA(in.SHA256) {
		return field("sha256", "invalid")
	}
	return nil
}
func validateUpload(in UploadInput) []operations.FieldError {
	if e := ID(in.UploadID, "upload_id"); len(e) > 0 {
		return e
	}
	return ID(in.DraftID, "draft_id")
}
func validateMutation(in VersionInput) []operations.FieldError {
	if e := validateUpload(UploadInput{in.UploadID, in.DraftID}); len(e) > 0 {
		return e
	}
	return validateVersion(in.ExpectedVersion)
}
func validateDraft(value string) []operations.FieldError {
	if value == "" {
		return nil
	}
	id, e := uuid.Parse(value)
	if e != nil || id == uuid.Nil || len(value) != 36 {
		return field("draft_id", "invalid")
	}
	return nil
}
func validateFile(in AttachmentInput) []operations.FieldError {
	if e := ID(in.AttachmentID, "attachment_id"); len(e) > 0 {
		return e
	}
	return validateDraft(in.DraftID)
}
func validatePage(limit int32, after string) []operations.FieldError {
	if limit < 1 || limit > 100 {
		return field("limit", "invalid")
	}
	if after != "" {
		if _, e := parseCursor(after); e != nil {
			return field("after", "invalid")
		}
	}
	return nil
}
func SafeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)
	filename = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, filename)
	if filename == "" || filename == "." || filename == ".." {
		return "download"
	}
	return filename
}
func ContentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": SafeFilename(filename)})
}
func (ChunkInput) ValidateWire(_ any, raw json.RawMessage) []operations.FieldError {
	if len(raw) > MaxChunkRequestBytes {
		return field("input", "bytes_limit")
	}
	return nil
}
func (ChunkInput) InputBudgetBytes() int64 { return MaxChunkRequestBytes }
