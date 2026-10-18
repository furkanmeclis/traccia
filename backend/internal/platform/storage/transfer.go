package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"io"
	"math"
)

const MaxBoundedObjectBytes = 2 << 20

// BoundedTransfer is separate from inherited whole-object explorer uploads.
// Every Put buffer is capped; precise range reads expose actual object length.
type BoundedTransfer interface {
	PutBounded(context.Context, string, []byte) error
	DownloadRange(context.Context, string, int64, int64) (io.ReadCloser, int64, error)
}

func (d *S3Driver) PutBounded(ctx context.Context, key string, data []byte) error {
	key, e := sanitizePath(key)
	if e != nil {
		return e
	}
	if len(data) > MaxBoundedObjectBytes {
		return fmt.Errorf("bounded object exceeds maximum")
	}
	_, e = d.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String("application/octet-stream"), IfNoneMatch: aws.String("*")})
	if e == nil {
		return nil
	}
	var api smithy.APIError
	if !errors.As(e, &api) || api.ErrorCode() != "PreconditionFailed" {
		return e
	}
	body, size, e := d.Download(ctx, key)
	if e != nil {
		return e
	}
	defer func() { _ = body.Close() }()
	if size != int64(len(data)) {
		return fmt.Errorf("immutable object collision")
	}
	actual, e := io.ReadAll(io.LimitReader(body, int64(len(data))+1))
	if e != nil {
		return e
	}
	if !bytes.Equal(actual, data) {
		return fmt.Errorf("immutable object collision")
	}
	return nil
}
func (d *S3Driver) DownloadRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, int64, error) {
	key, e := sanitizePath(key)
	if e != nil {
		return nil, 0, e
	}
	if offset < 0 || length < 1 || length > MaxBoundedObjectBytes || offset > math.MaxInt64-length {
		return nil, 0, fmt.Errorf("invalid bounded range")
	}
	out, e := d.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key), Range: aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))})
	if e != nil {
		return nil, 0, e
	}
	var start, end, total int64
	if out.ContentRange == nil {
		_ = out.Body.Close()
		return nil, 0, fmt.Errorf("range response missing bounds")
	}
	if _, e = fmt.Sscanf(*out.ContentRange, "bytes %d-%d/%d", &start, &end, &total); e != nil || start != offset || end < start || end-start+1 > length || total < end+1 || fmt.Sprintf("bytes %d-%d/%d", start, end, total) != *out.ContentRange {
		_ = out.Body.Close()
		return nil, 0, fmt.Errorf("invalid range response")
	}
	return out.Body, total, nil
}
func (m *Memory) PutBounded(_ context.Context, key string, data []byte) error {
	key, e := sanitizePath(key)
	if e != nil {
		return e
	}
	if len(data) > MaxBoundedObjectBytes {
		return fmt.Errorf("bounded object exceeds maximum")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.data[key]; ok {
		if !bytes.Equal(old.Body, data) {
			return fmt.Errorf("immutable object collision")
		}
		return nil
	}
	m.data[key] = memoryObject{Body: append([]byte(nil), data...), ContentType: "application/octet-stream"}
	return nil
}
func (m *Memory) DownloadRange(_ context.Context, key string, offset, length int64) (io.ReadCloser, int64, error) {
	key, e := sanitizePath(key)
	if e != nil {
		return nil, 0, e
	}
	if offset < 0 || length < 1 || length > MaxBoundedObjectBytes || offset > math.MaxInt64-length {
		return nil, 0, fmt.Errorf("invalid bounded range")
	}
	m.mu.RLock()
	value, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, 0, fmt.Errorf("object not found")
	}
	total := int64(len(value.Body))
	if offset >= total {
		return nil, total, fmt.Errorf("range outside object")
	}
	end := offset + length
	if end > total {
		end = total
	}
	return io.NopCloser(bytes.NewReader(value.Body[offset:end])), total, nil
}
