package service

import (
	"bytes"
	"container/heap"
	"encoding/binary"
	"errors"
	"math"
	"unsafe"
)

var ErrSearchCapacityExceeded = errors.New("search session capacity exceeded")
var ErrSearchContextChanged = errors.New("search context changed")

const searchUnknownRank = uint64(math.MaxUint64)
const searchDeletedSlot = uint32(math.MaxUint32)

type searchIdentity [32]byte

type searchRankedIdentity struct {
	ID          searchIdentity
	Ranks       [2]uint64
	KeywordKind uint8
}

type searchRRFRow struct {
	searchRankedIdentity
	Position int
	Class    int
	NextFree int
	Live     bool
}

// Fixed-size identities and explicitly allocated arrays make retained capacity
// measurable; built-in map bucket estimates are not a query memory budget.
// The three heaps contain exactly one active entry per pending row.
type searchRRF struct {
	rows              []searchRRFRow
	table             []uint32
	heaps             [3]searchRRFHeap
	free              int
	count, tombstones int
	ranks             [2]uint64
	done              [2]bool
	bytes             int64
	reserve           func(int64) error
}

type searchRRFHeap struct {
	store *searchRRF
	class int
	slots []uint32
}

func (h searchRRFHeap) Len() int { return len(h.slots) }
func (h searchRRFHeap) Less(i, j int) bool {
	a, b := h.store.rows[h.slots[i]], h.store.rows[h.slots[j]]
	if h.class == 0 {
		return searchRankLess(a.searchRankedIdentity, b.searchRankedIdentity)
	}
	rank := h.class - 1
	if a.Ranks[rank] != b.Ranks[rank] {
		return a.Ranks[rank] < b.Ranks[rank]
	}
	return bytes.Compare(a.ID[:], b.ID[:]) < 0
}
func (h searchRRFHeap) Swap(i, j int) {
	h.slots[i], h.slots[j] = h.slots[j], h.slots[i]
	h.store.rows[h.slots[i]].Position = i
	h.store.rows[h.slots[j]].Position = j
}
func (h *searchRRFHeap) Push(value any) {
	slot := value.(uint32)
	h.store.rows[slot].Position = len(h.slots)
	h.store.rows[slot].Class = h.class
	h.slots = append(h.slots, slot)
}
func (h *searchRRFHeap) Pop() any {
	n := len(h.slots) - 1
	slot := h.slots[n]
	h.slots = h.slots[:n]
	return slot
}

func newSearchRRF(reserve func(int64) error) *searchRRF {
	s := &searchRRF{free: -1, reserve: reserve}
	for c := range s.heaps {
		s.heaps[c] = searchRRFHeap{store: s, class: c}
	}
	return s
}
func searchRankScore(ranks [2]uint64) float64 {
	score := 0.
	for _, r := range ranks {
		if r > 0 && r != searchUnknownRank {
			score += 1. / (rrfRankConstant + float64(r))
		}
	}
	return score / (2. / (rrfRankConstant + 1.))
}
func searchRankLess(a, b searchRankedIdentity) bool {
	x, y := searchRankScore(a.Ranks), searchRankScore(b.Ranks)
	if x != y {
		return x > y
	}
	if (a.Ranks[0] > 0) != (b.Ranks[0] > 0) {
		return a.Ranks[0] > 0
	}
	return bytes.Compare(a.ID[:], b.ID[:]) < 0
}
func searchRankClass(ranks [2]uint64) int {
	if ranks[1] == searchUnknownRank {
		return 1
	}
	if ranks[0] == searchUnknownRank {
		return 2
	}
	return 0
}
func searchGrowCapacity(current, needed int) int {
	if current >= needed {
		return current
	}
	n := current * 2
	if n < 64 {
		n = 64
	}
	for n < needed {
		n *= 2
	}
	return n
}
func searchIdentityHash(id searchIdentity) uint64 { return binary.LittleEndian.Uint64(id[:8]) }
func (s *searchRRF) find(id searchIdentity) (uint32, int, bool) {
	if len(s.table) == 0 {
		return 0, 0, false
	}
	mask := len(s.table) - 1
	pos := int(searchIdentityHash(id) & uint64(mask))
	firstDeleted := -1
	for n := 0; n < len(s.table); n++ {
		entry := s.table[pos]
		if entry == 0 {
			if firstDeleted >= 0 {
				pos = firstDeleted
			}
			return 0, pos, false
		}
		if entry == searchDeletedSlot {
			if firstDeleted < 0 {
				firstDeleted = pos
			}
		} else if s.rows[entry-1].ID == id {
			return entry - 1, pos, true
		}
		pos = (pos + 1) & mask
	}
	return 0, firstDeleted, false
}
func (s *searchRRF) rebuild() {
	clear(s.table)
	s.tombstones = 0
	for slot := range s.rows {
		r := &s.rows[slot]
		if r.Live {
			_, pos, _ := s.find(r.ID)
			s.table[pos] = uint32(slot) + 1
		}
	}
}

// Reserve all needed storage before mutating a row or consuming its input.
func (s *searchRRF) prepare(class int, newRow bool) error {
	rowCapacity := cap(s.rows)
	tableCapacity := len(s.table)
	heapCapacity := cap(s.heaps[class].slots)
	if newRow {
		if s.free < 0 {
			rowCapacity = searchGrowCapacity(rowCapacity, len(s.rows)+1)
		}
		if tableCapacity == 0 || (s.count+1)*10 > tableCapacity*6 {
			tableCapacity = searchGrowCapacity(tableCapacity, (s.count+1)*2)
		}
	}
	heapCapacity = searchGrowCapacity(heapCapacity, len(s.heaps[class].slots)+1)
	delta := int64(rowCapacity-cap(s.rows))*int64(unsafe.Sizeof(searchRRFRow{})) + int64(tableCapacity-len(s.table))*4 + int64(heapCapacity-cap(s.heaps[class].slots))*4
	if delta > 0 {
		if err := s.reserve(delta); err != nil {
			return err
		}
		s.bytes += delta
	}
	if rowCapacity != cap(s.rows) {
		rows := make([]searchRRFRow, len(s.rows), rowCapacity)
		copy(rows, s.rows)
		s.rows = rows
	}
	if tableCapacity != len(s.table) {
		s.table = make([]uint32, tableCapacity)
		s.rebuild()
	} else if s.tombstones > len(s.table)/3 {
		s.rebuild()
	}
	if heapCapacity != cap(s.heaps[class].slots) {
		slots := make([]uint32, len(s.heaps[class].slots), heapCapacity)
		copy(slots, s.heaps[class].slots)
		s.heaps[class].slots = slots
	}
	return nil
}

func (s *searchRRF) accept(source int, id searchIdentity, keywordKind uint8) error {
	slot, _, found := s.find(id)
	ranks := [2]uint64{searchUnknownRank, searchUnknownRank}
	if found {
		ranks = s.rows[slot].Ranks
		if ranks[source] != searchUnknownRank {
			return ErrSearchContextChanged
		}
	} else if s.done[1-source] {
		ranks[1-source] = 0
	}
	ranks[source] = s.ranks[source] + 1
	class := searchRankClass(ranks)
	if err := s.prepare(class, !found); err != nil {
		return err
	}
	if found {
		heap.Remove(&s.heaps[s.rows[slot].Class], s.rows[slot].Position)
	} else {
		if s.free >= 0 {
			slot = uint32(s.free)
			s.free = s.rows[slot].NextFree
		} else {
			slot = uint32(len(s.rows))
			s.rows = append(s.rows, searchRRFRow{})
		}
		_, pos, _ := s.find(id)
		if s.table[pos] == searchDeletedSlot {
			s.tombstones--
		}
		s.table[pos] = slot + 1
		s.count++
		s.rows[slot] = searchRRFRow{searchRankedIdentity: searchRankedIdentity{ID: id}, Live: true}
	}
	s.rows[slot].Ranks = ranks
	if source == 0 {
		s.rows[slot].KeywordKind = keywordKind
	}
	s.ranks[source]++
	heap.Push(&s.heaps[class], slot)
	return nil
}

// Exhaustion is a source fact. Promotion remains bounded by the caller's work
// budget, so a large absent contribution does not force an unbounded page step.
func (s *searchRRF) promote(limit int) (int, error) {
	moved := 0
	for c := 1; c < 3; c++ {
		missing := 2 - c
		if !s.done[missing] {
			continue
		}
		for s.heaps[c].Len() > 0 && moved < limit {
			if err := s.prepare(0, false); err != nil {
				return moved, err
			}
			slot := heap.Pop(&s.heaps[c]).(uint32)
			s.rows[slot].Ranks[missing] = 0
			heap.Push(&s.heaps[0], slot)
			moved++
		}
	}
	return moved, nil
}
func (s *searchRRF) upper(ranks [2]uint64) float64 {
	for i := range ranks {
		if ranks[i] == searchUnknownRank {
			ranks[i] = 0
			if !s.done[i] {
				ranks[i] = s.ranks[i] + 1
			}
		}
	}
	return searchRankScore(ranks)
}
func (s *searchRRF) pop() (searchRankedIdentity, bool) {
	ready := &s.heaps[0]
	if ready.Len() == 0 {
		return searchRankedIdentity{}, false
	}
	unseen := [2]uint64{}
	for i := range unseen {
		if !s.done[i] {
			unseen[i] = s.ranks[i] + 1
		}
	}
	bound := searchRankScore(unseen)
	for c := 1; c < 3; c++ {
		if s.heaps[c].Len() > 0 {
			upper := s.upper(s.rows[s.heaps[c].slots[0]].Ranks)
			if upper > bound {
				bound = upper
			}
		}
	}
	row := s.rows[ready.slots[0]]
	if searchRankScore(row.Ranks) <= bound {
		return searchRankedIdentity{}, false
	}
	slot := heap.Pop(ready).(uint32)
	_, position, _ := s.find(row.ID)
	s.table[position] = searchDeletedSlot
	s.tombstones++
	s.count--
	s.rows[slot] = searchRRFRow{NextFree: s.free}
	s.free = int(slot)
	return row.searchRankedIdentity, true
}
