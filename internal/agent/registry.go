package agent

import (
	"fmt"
	"os"
	"sync"
)

// CachedIdentity holds the in-memory copy of an agent's three editable brain
// files. It is invalidated whenever any of those files is written to disk.
type CachedIdentity struct {
	Soul    string
	Persona string
	Skills  string
}

// identityEntry is the internal cache record — wraps CachedIdentity with a
// dirty flag that triggers a re-read on the next Get call.
type identityEntry struct {
	data  CachedIdentity
	dirty bool
}

// IdentityRegistry is a thread-safe in-memory cache of agent brain files
// (soul.md, persona.md, skills.md).
//
// Usage pattern:
//
//	reg := agent.NewIdentityRegistry()
//	id, err := reg.Get(paths, agentID)   // cache miss → reads disk; hit → returns cached
//	reg.Invalidate(agentID)               // mark dirty → next Get re-reads from disk
type IdentityRegistry struct {
	mu      sync.RWMutex
	entries map[string]*identityEntry
}

// NewIdentityRegistry creates an empty IdentityRegistry.
func NewIdentityRegistry() *IdentityRegistry {
	return &IdentityRegistry{
		entries: make(map[string]*identityEntry),
	}
}

// Get returns the cached brain files for agentID. If the entry is absent or
// marked dirty it reads the files from disk via paths, caches the result, and
// returns it. Returns an error if any file cannot be read.
func (r *IdentityRegistry) Get(paths IdentityPaths, agentID string) (CachedIdentity, error) {
	// Fast path: valid cache hit.
	r.mu.RLock()
	e, ok := r.entries[agentID]
	if ok && !e.dirty {
		data := e.data
		r.mu.RUnlock()
		return data, nil
	}
	r.mu.RUnlock()

	// Slow path: load from disk.
	data, err := loadFromDisk(paths)
	if err != nil {
		return CachedIdentity{}, err
	}

	r.mu.Lock()
	r.entries[agentID] = &identityEntry{data: data, dirty: false}
	r.mu.Unlock()

	return data, nil
}

// Set directly stores brain file content for agentID without reading from disk.
// Useful when content has just been written and is already available in memory.
func (r *IdentityRegistry) Set(agentID string, soul, persona, skills string) {
	r.mu.Lock()
	r.entries[agentID] = &identityEntry{
		data:  CachedIdentity{Soul: soul, Persona: persona, Skills: skills},
		dirty: false,
	}
	r.mu.Unlock()
}

// Invalidate marks the cached entry for agentID as dirty. The next call to
// Get will re-read the files from disk. Safe to call on a non-existent entry.
func (r *IdentityRegistry) Invalidate(agentID string) {
	r.mu.Lock()
	if e, ok := r.entries[agentID]; ok {
		e.dirty = true
	} else {
		// Insert a dirty sentinel so Get knows to load from disk.
		r.entries[agentID] = &identityEntry{dirty: true}
	}
	r.mu.Unlock()
}

// loadFromDisk reads the three brain files and returns a CachedIdentity.
func loadFromDisk(paths IdentityPaths) (CachedIdentity, error) {
	soul, err := os.ReadFile(paths.SoulPath)
	if err != nil {
		return CachedIdentity{}, fmt.Errorf("registry: read soul.md: %w", err)
	}
	persona, err := os.ReadFile(paths.PersonaPath)
	if err != nil {
		return CachedIdentity{}, fmt.Errorf("registry: read persona.md: %w", err)
	}
	skills, err := os.ReadFile(paths.SkillsPath)
	if err != nil {
		return CachedIdentity{}, fmt.Errorf("registry: read skills.md: %w", err)
	}
	return CachedIdentity{
		Soul:    string(soul),
		Persona: string(persona),
		Skills:  string(skills),
	}, nil
}
