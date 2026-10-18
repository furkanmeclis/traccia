package usecase

import (
	"errors"
	"path"
	"strings"

	platstorage "github.com/furkanmeclis/traccia/backend/internal/platform/storage"
)

var (
	ErrNotFound    = errors.New("storage object not found")
	ErrInvalidKey  = errors.New("invalid object key")
	ErrConflict    = errors.New("object already exists")
	ErrForbidden   = errors.New("storage access denied")
	ErrLinkExpired = errors.New("link expired")
	ErrLinkRevoked = errors.New("link revoked")
)

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "/"))
	if strings.Contains(prefix, "..") || attachmentPrivateKey(prefix) {
		return "", ErrInvalidKey
	}
	if prefix == "" {
		return "", nil
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix, nil
}

func normalizeKey(key string) (string, error) {
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if key == "" || strings.Contains(key, "..") || attachmentPrivateKey(key) {
		return "", ErrInvalidKey
	}
	return key, nil
}

func baseName(key string) string {
	key = strings.TrimSuffix(key, "/")
	if key == "" {
		return ""
	}
	return path.Base(key)
}

func parentPrefix(key string) string {
	key = strings.TrimSuffix(strings.TrimPrefix(key, "/"), "/")
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return ""
	}
	return key[:i+1]
}

func joinKey(prefix, name string) (string, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return "", err
	}
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return "", ErrInvalidKey
	}
	return prefix + name, nil
}

func isFolderKey(key string) bool {
	return strings.HasSuffix(key, "/")
}

func folderKey(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasSuffix(key, "/") {
		return key
	}
	return key + "/"
}

func skipSystem(key string) bool {
	return platstorage.IsSystemKey(key)
}

// Product attachments have their own workspace authorizer and immutable object
// authority. Platform explorer grants never authorize this private namespace.
func attachmentPrivateKey(key string) bool {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	root := platstorage.SystemPrefix + "attachments"
	return key == root || strings.HasPrefix(key, root+"/")
}
