package usecase

import (
	"fmt"
	"time"
)

const MaxChunks = 4096
const MaxChunkRequestBytes = 512 << 10

func DefaultOptions() Options {
	return Options{MaxFileBytes: MaxFileBytes, ChunkBytes: MaxChunkBytes, UploadTTL: DefaultUploadTTL, FinalizedRetention: DefaultFinalizedRetention, CleanupInterval: time.Hour}
}
func (o Options) Validate() error {
	if o.MaxFileBytes < 0 || o.MaxFileBytes > MaxFileBytes || o.ChunkBytes < 1 || o.ChunkBytes > MaxChunkBytes || o.MaxFileBytes > int64(o.ChunkBytes)*MaxChunks || o.UploadTTL < time.Second || o.FinalizedRetention < time.Second || o.CleanupInterval < time.Second {
		return fmt.Errorf("invalid attachment limits or expiry configuration")
	}
	return nil
}
func (o Options) Limits() Limits {
	return Limits{MaxChunkRequestBytes: MaxChunkRequestBytes, MaxFileBytes: o.MaxFileBytes, ChunkBytes: o.ChunkBytes, UploadInactivitySeconds: int64(o.UploadTTL / time.Second), FinalizedRetentionSeconds: int64(o.FinalizedRetention / time.Second), BlobFallbackMaxBytes: o.MaxFileBytes}
}
