package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

func brainAvailable() error {
	if memEncActive() && !memUnlocked() {
		return errMemLocked
	}
	return nil
}
func brainMemory(ctx context.Context, emit func(brain.Document) bool) error {
	if err := brainAvailable(); err != nil {
		return err
	}
	root, err := os.OpenRoot(memoryDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	visit := func(path string, entry fs.DirEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			if path != "." {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > brain.MaxFileBytes {
			return nil
		}
		// Root-confined read followed by the existing memory decryption layer.
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(file, brain.MaxFileBytes+1))
		file.Close()
		if err != nil {
			return err
		}
		if len(b) > brain.MaxFileBytes {
			return nil
		}
		b, err = decodeMemContent(b)
		if err != nil {
			return err
		}
		if !emit(brain.Document{Path: path, Text: string(b)}) {
			return fs.SkipAll
		}
		return nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if err := visit(entry.Name(), entry); err != nil {
				if err == fs.SkipAll {
					return nil
				}
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func brainMessages(ctx context.Context, prefix, title string, messages []Message, emit func(brain.Document) bool) bool {
	for i, msg := range messages {
		if ctx.Err() != nil {
			return false
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		text, ok := msg.Content.(string)
		if !ok || text == "" || len(text) > brain.MaxFileBytes {
			continue
		}
		// No tools, approvals, hidden reasoning, instructions or usage metadata.
		if !emit(brain.Document{Path: fmt.Sprintf("%s/%06d-%s.md", prefix, i, msg.Role), Text: "# " + strings.ReplaceAll(title, "\n", " ") + "\n\n" + text}) {
			return false
		}
	}
	return true
}
func brainConversations(ctx context.Context, emit func(brain.Document) bool) error {
	if err := brainAvailable(); err != nil {
		return err
	}
	activeID := ""
	// Current native journal and archives are the full user-visible transcript.
	// Bound sessions are skipped below to avoid indexing the same native text twice.
	if a := conv.snapshotForSession(); a != nil {
		activeID = a.ID
		if !brainMessages(ctx, "native/"+a.ID, a.Title, archivePortableText(a), emit) {
			return ctx.Err()
		}
	}
	complete, archivesErr := brainRecords(ctx, bkChatHist, func(id string, data []byte) bool {
		if id == activeID {
			return true
		}
		var a convArchive
		if json.Unmarshal(data, &a) != nil || a.ID != id {
			return true
		}
		return brainMessages(ctx, "native/"+a.ID, a.Title, archivePortableText(&a), emit)
	})
	if !complete {
		return archivesErr
	}
	_, sessionsErr := brainRecords(ctx, bkRuntimeSessions, func(id string, data []byte) bool {
		// Decode only portable text; do not allocate private harness/tool state.
		var s struct {
			ID            string    `json:"id"`
			Title         string    `json:"title"`
			NativeArchive string    `json:"native_archive"`
			Messages      []Message `json:"messages"`
		}
		if json.Unmarshal(data, &s) != nil || s.ID != id || s.NativeArchive != "" {
			return true
		}
		workspaceSessions.mu.Lock()
		if run := workspaceSessions.runs[id]; run != nil {
			s.Title = run.session.Title
			s.Messages = append([]Message(nil), run.session.Messages...)
		}
		workspaceSessions.mu.Unlock()
		return brainMessages(ctx, "discussion/"+s.ID, s.Title, s.Messages, emit)
	})
	return errors.Join(archivesErr, sessionsErr)
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
