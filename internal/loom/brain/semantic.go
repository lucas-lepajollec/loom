package brain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sort"
)

// Vectors are derived data, keyed by content-addressed chunk IDs and a model
// identity. Keeping only live IDs invalidates edits, removals and scope changes.
type Vectors struct {
	Identity string
	Values   map[string][]float32
}

func (v *Vectors) Prune(chunks []Chunk) {
	live := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		live[c.ID] = true
	}
	for id := range v.Values {
		if !live[id] {
			delete(v.Values, id)
		}
	}
}
func ValidVector(v []float32) bool {
	if len(v) == 0 || len(v) > 4096 {
		return false
	}
	norm := float64(0)
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		norm += float64(x) * float64(x)
	}
	return norm > 0
}
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || !ValidVector(a) || !ValidVector(b) {
		return 0
	}
	var dot, aa, bb float64
	for i, x := range a {
		y := b[i]
		dot += float64(x) * float64(y)
		aa += float64(x) * float64(x)
		bb += float64(y) * float64(y)
	}
	return dot / math.Sqrt(aa*bb)
}
func (v Vectors) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("LOOMV1")
	if len(v.Identity) > 4096 || len(v.Values) > MaxChunks {
		return nil, errors.New("vector store exceeds limits")
	}
	binary.Write(&b, binary.LittleEndian, uint32(len(v.Identity)))
	b.WriteString(v.Identity)
	binary.Write(&b, binary.LittleEndian, uint32(len(v.Values)))
	ids := []string{}
	for id := range v.Values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		vec := v.Values[id]
		if len(id) > 256 || !ValidVector(vec) {
			return nil, errors.New("invalid vector")
		}
		binary.Write(&b, binary.LittleEndian, uint32(len(id)))
		b.WriteString(id)
		binary.Write(&b, binary.LittleEndian, uint32(len(vec)))
		binary.Write(&b, binary.LittleEndian, vec)
	}
	return b.Bytes(), nil
}
func (v *Vectors) UnmarshalBinary(data []byte) error {
	r := bytes.NewReader(data)
	magic := make([]byte, 6)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "LOOMV1" {
		return errors.New("invalid vector store")
	}
	readString := func(limit uint32) (string, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return "", err
		}
		if n > limit || int(n) > r.Len() {
			return "", errors.New("invalid vector length")
		}
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return string(b), err
	}
	identity, err := readString(4096)
	if err != nil {
		return err
	}
	var count uint32
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		return err
	}
	if count > MaxChunks {
		return errors.New("too many vectors")
	}
	next := Vectors{Identity: identity, Values: map[string][]float32{}}
	dim := 0
	for range count {
		id, err := readString(256)
		if err != nil {
			return err
		}
		var n uint32
		if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
			return err
		}
		if n == 0 || n > 4096 || int(n)*4 > r.Len() {
			return errors.New("invalid vector dimension")
		}
		vec := make([]float32, n)
		if err = binary.Read(r, binary.LittleEndian, vec); err != nil {
			return err
		}
		if !ValidVector(vec) || (dim != 0 && dim != len(vec)) {
			return errors.New("invalid vector values")
		}
		dim = len(vec)
		if _, ok := next.Values[id]; ok {
			return errors.New("duplicate vector")
		}
		next.Values[id] = vec
	}
	if r.Len() != 0 {
		return errors.New("trailing vector data")
	}
	*v = next
	return nil
}

// Chunks applies exactly the same source/privacy policy as lexical search.
func (e *Engine) Chunks(r SearchRequest) ([]Chunk, error) {
	if err := validateScope(r.PathPrefixes); err != nil {
		return nil, err
	}
	if err := e.available(); err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	allowed, err := e.allowed(r.Sources, r.Personal)
	if err != nil {
		return nil, err
	}
	out := []Chunk{}
	for _, d := range e.docs {
		if allowed[d.chunk.Source] && allowsPath(r.PathPrefixes, d.chunk.Source, d.chunk.Path) {
			c := *d.chunk
			c.Heading = append([]string{}, c.Heading...)
			out = append(out, c)
		}
	}
	return out, nil
}

// Fuse implements reciprocal rank fusion (k=60), with stable ID tie breaks.
func Fuse(lists ...[]Hit) []Hit {
	byID := map[string]Hit{}
	for _, list := range lists {
		seen := map[string]bool{}
		for rank, h := range list {
			if seen[h.ChunkID] {
				continue
			}
			seen[h.ChunkID] = true
			prior, ok := byID[h.ChunkID]
			if !ok {
				prior = h
				prior.Score = 0
			}
			prior.Score += 1 / float64(60+rank+1)
			byID[h.ChunkID] = prior
		}
	}
	out := []Hit{}
	for _, h := range byID {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].Score > out[j].Score
	})
	return out
}
func (e *Engine) HybridSearch(r SearchRequest, query []float32, vectors map[string][]float32) ([]Hit, error) {
	if err := e.available(); err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	limit := r.Limit
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 100)
	r.Limit = 100
	lexical, err := e.searchLocked(r)
	if err != nil {
		return nil, err
	}
	if len(queryTerms(r.Query)) == 0 || !ValidVector(query) {
		return lexical[:min(limit, len(lexical))], nil
	}
	allowed, err := e.allowed(r.Sources, r.Personal)
	if err != nil {
		return nil, err
	}
	semantic := []Hit{}
	for _, d := range e.docs {
		c := d.chunk
		vec := vectors[c.ID]
		if !allowed[c.Source] || !allowsPath(r.PathPrefixes, c.Source, c.Path) || len(vec) != len(query) || !ValidVector(vec) {
			continue
		}
		score := Cosine(query, vec)
		if score <= 0 {
			continue
		}
		text, ranges := snippet(c.Text, queryTerms(r.Query))
		semantic = append(semantic, Hit{c.Source, c.Path, append([]string{}, c.Heading...), text, ranges, score, c.ID})
	}
	if len(semantic) == 0 {
		return lexical[:min(limit, len(lexical))], nil
	}
	sort.Slice(semantic, func(i, j int) bool {
		if semantic[i].Score == semantic[j].Score {
			return semantic[i].ChunkID < semantic[j].ChunkID
		}
		return semantic[i].Score > semantic[j].Score
	})
	fused := Fuse(lexical, semantic[:min(100, len(semantic))])
	return fused[:min(limit, len(fused))], nil
}
