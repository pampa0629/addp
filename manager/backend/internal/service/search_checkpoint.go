package service

import (
	"bytes"

	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"math"
	"unsafe"

	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

// This is the complete retained wire format. HTTP data, query text, decisions,
// credentials, query embeddings, clients and evictable segment caches are absent.
type searchCheckpoint struct {
	Query, Material, Observation, Model, Vector [32]byte
	Nonce, Last, Next                           uuid.UUID
	PageSize                                    int
	Engine, Tenant                              uint
	High, SegmentSize                           int64
	Exhausted                                   bool
	Delivered                                   []searchRankedIdentity
	Ranks                                       [2]uint64
	Done                                        [2]bool
	PendingRanks                                []byte
	Streams                                     [2]checkpointStream
	Keywords                                    [2]checkpointKeyword
	VectorAfter                                 repository.SearchVectorPosition
	VectorHasAfter, VectorDone                  bool
}

type checkpointKeyword struct {
	Probed, Initialized bool
	Next                int64
	Leaves              searchLeafHeap
}

type checkpointHit struct {
	Candidate searchSourceCandidate
	Locator   [32]byte
	Engine    uint
	Kind      uint8
}

type checkpointStream struct {
	Pending, Buffered []checkpointHit
	Exhausted         bool
}

func vectorFingerprint(q *repository.SearchVectorQuery) (out [32]byte) {
	if q == nil {
		return
	}
	h := sha256.New()
	var word [4]byte
	for _, v := range q.Vector {
		binary.LittleEndian.PutUint32(word[:], math.Float32bits(v))
		h.Write(word[:])
	}
	copy(out[:], h.Sum(nil))
	return
}

func checkpointHits(src []searchSourceHit) []checkpointHit {
	hits := make([]checkpointHit, len(src))
	for i, v := range src {
		hits[i] = checkpointHit{v.searchSourceCandidate, v.ExpectedLocator, v.ExpectedEngine, v.Kind}
	}
	return hits
}

func (s *searchSession) encodeCheckpoint() ([]byte, error) {
	c := searchCheckpoint{Query: sha256.Sum256([]byte(s.query)), Material: s.material, Observation: s.observationTag, Model: s.modelKey, Vector: vectorFingerprint(s.vectorQuery), Nonce: s.nonce, Last: s.lastInput, Next: s.lastNext, PageSize: s.pageSize, Engine: s.engine, Tenant: s.tenant, High: s.high, SegmentSize: s.segmentSize, Exhausted: s.lastExhausted, Delivered: s.last}
	if !c.Exhausted {
		r := s.rrf
		c.Ranks, c.Done = r.ranks, r.done
		c.PendingRanks = make([]byte, 0, r.count*49)
		for _, row := range r.rows {
			if !row.Live {
				continue
			}
			c.PendingRanks = append(c.PendingRanks, row.ID[:]...)
			c.PendingRanks = binary.BigEndian.AppendUint64(c.PendingRanks, row.Ranks[0])
			c.PendingRanks = binary.BigEndian.AppendUint64(c.PendingRanks, row.Ranks[1])
			c.PendingRanks = append(c.PendingRanks, row.KeywordKind)
		}
		for i, source := range s.streams {
			stream := source.(*searchAuthorizedStream)
			c.Streams[i] = checkpointStream{checkpointHits(stream.pending), checkpointHits(stream.buffer[stream.position:]), stream.exhausted}
		}
		k := s.streams[0].(*searchAuthorizedStream).raw.(*searchRawKeyword)
		for i, src := range []*searchNativeKeyword{k.metadata, k.content} {
			c.Keywords[i] = checkpointKeyword{src.rootProbed, src.initialized, src.nextSegment, src.leaves}
		}
		v := s.streams[1].(*searchAuthorizedStream).raw.(*searchRawVector)
		c.VectorAfter, c.VectorHasAfter, c.VectorDone = v.after, v.hasAfter, v.done
	}
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(c)
	return b.Bytes(), err
}

func (c *searchCheckpoint) restore(s *searchSession) error {
	if c.Query != sha256.Sum256([]byte(s.query)) || c.Tenant != s.tenant || c.Engine != s.engine || c.PageSize != s.pageSize {
		return ErrSearchContinuationParameters
	}
	s.nonce, s.material, s.observationTag, s.observationSet = c.Nonce, c.Material, c.Observation, true
	s.lastInput, s.lastNext, s.currentCursor, s.lastExhausted = c.Last, c.Next, c.Next, c.Exhausted
	s.last = append(s.last[:0], c.Delivered...)
	s.high, s.segmentSize = c.High, c.SegmentSize
	if c.Exhausted {
		return nil
	}
	if len(c.PendingRanks)%49 != 0 {
		return errors.New("invalid search checkpoint ranks")
	}
	r := s.rrf
	for data := c.PendingRanks; len(data) > 0; data = data[49:] {
		row := searchRankedIdentity{ID: searchIdentity(data[:32]), Ranks: [2]uint64{binary.BigEndian.Uint64(data[32:40]), binary.BigEndian.Uint64(data[40:48])}, KeywordKind: data[48]}
		if err := r.putAt(row, 0, false); err != nil {
			return err
		}
	}
	r.ranks, r.done = c.Ranks, c.Done
	if err := s.initializeStreams(); err != nil {
		return err
	}
	for i, src := range c.Streams {
		stream := s.streams[i].(*searchAuthorizedStream)
		restore := func(values []checkpointHit) ([]searchSourceHit, int64, error) {
			if len(values) == 0 {
				return nil, 0, nil
			}
			size := int64(len(values)) * int64(unsafe.Sizeof(searchSourceHit{}))
			if err := s.reserve(size); err != nil {
				return nil, 0, err
			}
			hits := make([]searchSourceHit, len(values))
			for j, v := range values {
				hits[j] = searchSourceHit{v.Candidate, v.Locator, v.Engine, v.Kind}
			}
			return hits, size, nil
		}
		var err error
		if stream.pending, stream.pendingBytes, err = restore(src.Pending); err != nil {
			return err
		}
		if stream.buffer, stream.bufferBytes, err = restore(src.Buffered); err != nil {
			return err
		}
		stream.exhausted = src.Exhausted
	}
	k := s.streams[0].(*searchAuthorizedStream).raw.(*searchRawKeyword)
	for i, dst := range []*searchNativeKeyword{k.metadata, k.content} {
		v := c.Keywords[i]
		if err := s.reserve(int64(cap(v.Leaves)) * int64(unsafe.Sizeof(searchLeaf{}))); err != nil {
			return err
		}
		dst.rootProbed, dst.initialized, dst.nextSegment, dst.leaves = v.Probed, v.Initialized, v.Next, v.Leaves
	}
	v := s.streams[1].(*searchAuthorizedStream).raw.(*searchRawVector)
	if err := s.reserve(int64(len(c.VectorAfter.Fingerprint))); err != nil {
		return err
	}
	v.after, v.hasAfter, v.done = c.VectorAfter, c.VectorHasAfter, c.VectorDone
	return nil
}
