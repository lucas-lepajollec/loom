package loom

import (
	"bytes"
	"context"
	"errors"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

func brainAvailable() error {
	if memEncActive() && !memUnlocked() {
		return errMemLocked
	}
	return nil
}

// Read bounded batches, releasing the DB before calling the provider. The
// normal archive/session lists copy entire buckets (including private state)
// and may migrate metadata; a read-only context refresh must do neither.
const brainRecordBytes = 16 << 20

func brainRecords(ctx context.Context, bucket string, visit func(string, []byte) bool) (bool, error) {
	type record struct {
		id  string
		raw []byte
	}
	var after []byte
	var partial error
	for {
		batch := []record{}
		done := true
		err := store.View(dbPath(), bucket, func(b *bolt.Bucket) error {
			cursor := b.Cursor()
			key, value := cursor.First()
			if after != nil {
				key, value = cursor.Seek(after)
				if bytes.Equal(key, after) {
					key, value = cursor.Next()
				}
			}
			size, scanned := 0, 0
			for ; key != nil && scanned < 128; key, value = cursor.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if len(value) <= brainRecordBytes && size+len(value) > brainRecordBytes {
					break
				}
				after = append(after[:0], key...)
				scanned++
				if len(value) > brainRecordBytes {
					partial = errors.New("conversation record exceeds 16 MiB")
					continue
				}
				size += len(value)
				batch = append(batch, record{string(key), append([]byte(nil), value...)})
			}
			done = key == nil
			return nil
		})
		if err != nil {
			return false, errors.Join(partial, err)
		}
		// View does not call its callback for an absent bucket.
		if after == nil {
			return true, partial
		}
		for _, item := range batch {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			data, err := decodeMemContent(item.raw)
			if err != nil {
				return false, errors.Join(partial, err)
			}
			if !visit(item.id, data) {
				return false, errors.Join(partial, ctx.Err())
			}
		}
		if done {
			return true, partial
		}
	}
}
