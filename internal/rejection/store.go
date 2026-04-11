// Package rejection records exchanges discarded by the write pipeline quality
// gates. The bounded store feeds back into the ContentScorer as learned noise
// prototypes, replacing static heuristics with embeddings of real rejections.
package rejection

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Stage labels which gate caught the exchange.
const (
	StagePreFilter   = "pre_filter"  // cheap heuristic fired before LLM call
	StageSynthesizer = "synthesizer" // LLM returned SKIP sentinel
)

const (
	// DefaultMaxSize is the default capacity of the ring buffer.
	DefaultMaxSize = 500

	// RebuildEvery is the number of new entries between scorer rebuild signals.
	// Lower values make the scorer more reactive; higher values batch the cost.
	RebuildEvery = 25

	// asstTextLen is how many chars of assistant text to retain per entry.
	// 500 chars gives the embedder enough context for a meaningful noise vector.
	asstTextLen = 500
)

// Entry is one logged rejection.
type Entry struct {
	Time       time.Time `json:"time"`
	Stage      string    `json:"stage"`
	UserLen    int       `json:"user_len"`
	AsstLen    int       `json:"asst_len"`
	UserPrefix string    `json:"user_prefix"` // first 100 chars of user message
	AsstText   string    `json:"asst_text"`   // first 500 chars of assistant text (for embedding)
}

// Stats summarises the store.
type Stats struct {
	Total           int            `json:"total"`
	Capacity        int            `json:"capacity"`
	ByStage         map[string]int `json:"by_stage"`
	AvgUserLen      float64        `json:"avg_user_len"`
	AvgAsstLen      float64        `json:"avg_asst_len"`
	TopAsstPrefixes []PrefixCount  `json:"top_asst_prefixes"`
}

// PrefixCount pairs a first-word token with its frequency.
type PrefixCount struct {
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
}

// Store is a bounded ring buffer of rejected exchanges, backed by a JSONL file.
// All methods are goroutine-safe. A nil *Store is a no-op.
type Store struct {
	mu                sync.Mutex
	entries           []Entry
	maxSize           int
	path              string
	addedSinceRebuild int
	rebuildCh         chan struct{}
}

// Open loads (or creates) the rejection store at path with the given capacity.
// Pass maxSize <= 0 to use DefaultMaxSize.
func Open(path string, maxSize int) (*Store, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	s := &Store{
		maxSize:   maxSize,
		path:      path,
		rebuildCh: make(chan struct{}, 1),
	}

	// Load existing entries, keeping only the most recent maxSize.
	if entries, err := loadJSONL(path); err == nil {
		if len(entries) > maxSize {
			entries = entries[len(entries)-maxSize:]
		}
		s.entries = entries
	}

	return s, nil
}

// Add records a rejected exchange. When the store is at capacity the oldest
// entry is evicted (ring buffer). Every RebuildEvery additions a non-blocking
// signal is sent on RebuildCh so callers can update the ContentScorer.
func (s *Store) Add(stage, userMsg, asstMsg string) {
	if s == nil {
		return
	}
	e := Entry{
		Time:       time.Now().UTC(),
		Stage:      stage,
		UserLen:    len(userMsg),
		AsstLen:    len(asstMsg),
		UserPrefix: trunc(userMsg, 100),
		AsstText:   trunc(asstMsg, asstTextLen),
	}

	s.mu.Lock()
	if len(s.entries) >= s.maxSize {
		// Pop oldest from head.
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
	}
	s.entries = append(s.entries, e)
	s.addedSinceRebuild++
	shouldSignal := s.addedSinceRebuild >= RebuildEvery
	if shouldSignal {
		s.addedSinceRebuild = 0
	}
	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	s.mu.Unlock()

	// Persist asynchronously so the hot path is not blocked.
	go func() { _ = persistJSONL(s.path, entries) }()

	if shouldSignal {
		// Non-blocking: if the channel already has an item, the receiver hasn't
		// caught up yet — no need to queue another.
		select {
		case s.rebuildCh <- struct{}{}:
		default:
		}
	}
}

// RebuildCh returns the channel that receives a signal every RebuildEvery adds.
// Callers should drain this channel and rebuild the ContentScorer when it fires.
func (s *Store) RebuildCh() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.rebuildCh
}

// Texts returns the stored assistant texts, suitable for use as noise
// prototypes when embedding. The returned slice is a snapshot copy.
func (s *Store) Texts() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.AsstText
	}
	return out
}

// Len returns the current number of stored entries.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Stats computes aggregate statistics over the stored entries.
func (s *Store) Stats() Stats {
	if s == nil {
		return Stats{ByStage: map[string]int{}, Capacity: DefaultMaxSize}
	}
	s.mu.Lock()
	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	maxSize := s.maxSize
	s.mu.Unlock()

	st := Stats{
		Total:    len(entries),
		Capacity: maxSize,
		ByStage:  map[string]int{},
	}
	if len(entries) == 0 {
		return st
	}

	prefixFreq := map[string]int{}
	var sumUser, sumAsst float64
	for _, e := range entries {
		st.ByStage[e.Stage]++
		sumUser += float64(e.UserLen)
		sumAsst += float64(e.AsstLen)
		if w := firstWord(e.AsstText); w != "" {
			prefixFreq[w]++
		}
	}
	n := float64(len(entries))
	st.AvgUserLen = math.Round(sumUser/n*10) / 10
	st.AvgAsstLen = math.Round(sumAsst/n*10) / 10

	type kv struct {
		k string
		v int
	}
	kvs := make([]kv, 0, len(prefixFreq))
	for k, v := range prefixFreq {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
	limit := 15
	if len(kvs) < limit {
		limit = len(kvs)
	}
	for _, kv := range kvs[:limit] {
		st.TopAsstPrefixes = append(st.TopAsstPrefixes, PrefixCount{Prefix: kv.k, Count: kv.v})
	}
	return st
}

// Sample returns up to n most recent entries.
func (s *Store) Sample(n int) []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) <= n {
		out := make([]Entry, len(s.entries))
		copy(out, s.entries)
		return out
	}
	out := make([]Entry, n)
	copy(out, s.entries[len(s.entries)-n:])
	return out
}

// Close flushes the current state to disk and releases resources.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	s.mu.Unlock()
	return persistJSONL(s.path, entries)
}

// --- persistence helpers ---

func loadJSONL(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

func persistJSONL(path string, entries []Entry) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, e := range entries {
		data, _ := json.Marshal(e)
		if _, err := w.Write(data); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// --- string helpers ---

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	i := strings.IndexAny(s, " \t\n.,!?;:")
	if i < 0 {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:i])
}
