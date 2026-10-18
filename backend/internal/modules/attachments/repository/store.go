package repository

import (
	"context"
	"fmt"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"io"
	"strings"
)

type ObjectStore struct {
	driver  storage.Driver
	bounded storage.BoundedTransfer
}

func NewObjectStore(driver storage.Driver) (*ObjectStore, error) {
	bounded, ok := driver.(storage.BoundedTransfer)
	if !ok {
		return nil, fmt.Errorf("attachment storage requires bounded transfer")
	}
	return &ObjectStore{driver, bounded}, nil
}
func (s *ObjectStore) Put(ctx context.Context, key string, data []byte) error {
	return s.bounded.PutBounded(ctx, key, data)
}
func (s *ObjectStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, int64, error) {
	return s.bounded.DownloadRange(ctx, key, offset, length)
}
func (s *ObjectStore) DeletePrefix(ctx context.Context, prefix string) error {
	if !strings.HasPrefix(prefix, storage.SystemPrefix+"attachments/") {
		return fmt.Errorf("attachment cleanup requires private prefix")
	}
	token := ""
	seen := map[string]bool{}
	for {
		page, e := s.driver.List(ctx, storage.ListObjectsInput{Prefix: prefix, ContinuationToken: token, MaxKeys: 1000})
		if e != nil {
			return e
		}
		for _, object := range page.Objects {
			if !strings.HasPrefix(object.Key, prefix) {
				return fmt.Errorf("attachment cleanup returned a foreign prefix")
			}
			if e = s.driver.Delete(ctx, object.Key); e != nil {
				return e
			}
		}
		if !page.IsTruncated {
			return nil
		}
		if page.NextContinuationToken == "" || seen[page.NextContinuationToken] {
			return fmt.Errorf("incomplete attachment cleanup listing")
		}
		token = page.NextContinuationToken
		seen[token] = true
		if e = ctx.Err(); e != nil {
			return e
		}
	}
}

var _ uc.Store = (*ObjectStore)(nil)
