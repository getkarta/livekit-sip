package customsip

import (
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
)

// SourceConfig holds extractor + routes for one source CIDR or the default bucket.
type SourceConfig struct {
	Extractor FlowExtractor
	Routes    map[string]string
}

// Config is the in-memory custom-SIP config pushed from voice-agent.
type Config struct {
	Sources map[string]SourceConfig
	Default *SourceConfig
}

type sourceBucket struct {
	cidr      string
	net       *net.IPNet
	extractor FlowExtractor
	routes    map[string]string
}

// Store holds source-scoped extractors and route maps for the INVITE hot path.
type Store struct {
	mu            sync.RWMutex
	sources       []sourceBucket
	defaultBucket sourceBucket
}

// NewStore returns an empty custom-SIP store.
func NewStore() *Store {
	return &Store{}
}

// ReplaceAll atomically replaces the entire config. Nil/empty clears entries.
func (s *Store) ReplaceAll(cfg Config) {
	nextSources := make([]sourceBucket, 0, len(cfg.Sources))
	for cidr, entry := range cfg.Sources {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(normalizeCIDR(cidr))
		if err != nil {
			continue
		}
		nextSources = append(nextSources, sourceBucket{
			cidr:      cidr,
			net:       ipNet,
			extractor: entry.Extractor.normalized(),
			routes:    cloneRoutes(entry.Routes),
		})
	}
	slices.SortFunc(nextSources, func(a, b sourceBucket) int {
		aOnes, _ := a.net.Mask.Size()
		bOnes, _ := b.net.Mask.Size()
		return bOnes - aOnes
	})

	var defaultBucket sourceBucket
	if cfg.Default != nil {
		defaultBucket.extractor = cfg.Default.Extractor.normalized()
		defaultBucket.routes = cloneRoutes(cfg.Default.Routes)
	}

	s.mu.Lock()
	s.sources = nextSources
	s.defaultBucket = defaultBucket
	s.mu.Unlock()
}

// LookupExtractor returns the extractor for sourceIP (longest CIDR match, then default).
func (s *Store) LookupExtractor(sourceIP string) FlowExtractor {
	if s == nil {
		return DefaultFlowExtractor
	}
	ip := net.ParseIP(strings.TrimSpace(sourceIP))
	if ip == nil {
		return s.fallbackExtractor()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.sources {
		if entry.net.Contains(ip) && entry.extractor.valid() {
			return entry.extractor
		}
	}
	return s.defaultExtractorLocked()
}

// LookupRoute returns route_key for flowID scoped to sourceIP (matched source, then default).
func (s *Store) LookupRoute(sourceIP, flowID string) (string, bool) {
	if s == nil || flowID == "" {
		return "", false
	}
	ip := net.ParseIP(strings.TrimSpace(sourceIP))

	s.mu.RLock()
	defer s.mu.RUnlock()
	if ip != nil {
		for _, entry := range s.sources {
			if !entry.net.Contains(ip) {
				continue
			}
			if routeKey, ok := entry.routes[flowID]; ok && routeKey != "" {
				return routeKey, true
			}
			break
		}
	}
	if routeKey, ok := s.defaultBucket.routes[flowID]; ok && routeKey != "" {
		return routeKey, true
	}
	return "", false
}

func (s *Store) fallbackExtractor() FlowExtractor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultExtractorLocked()
}

func (s *Store) defaultExtractorLocked() FlowExtractor {
	if s.defaultBucket.extractor.valid() {
		return s.defaultBucket.extractor
	}
	return DefaultFlowExtractor
}

// Size returns the total number of route entries across all buckets.
func (s *Store) Size() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.defaultBucket.routes)
	for _, entry := range s.sources {
		n += len(entry.routes)
	}
	return n
}

// Snapshot returns a copy of the current config for GET responses.
func (s *Store) Snapshot() Config {
	if s == nil {
		return Config{Sources: map[string]SourceConfig{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := Config{Sources: make(map[string]SourceConfig, len(s.sources))}
	for _, entry := range s.sources {
		src := SourceConfig{Routes: maps.Clone(entry.routes)}
		if entry.extractor.valid() {
			src.Extractor = entry.extractor
		}
		out.Sources[entry.cidr] = src
	}
	if len(s.defaultBucket.routes) > 0 || s.defaultBucket.extractor.valid() {
		out.Default = &SourceConfig{
			Extractor: s.defaultBucket.extractor,
			Routes:    maps.Clone(s.defaultBucket.routes),
		}
	}
	return out
}

func cloneRoutes(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for flowID, routeKey := range in {
		if flowID == "" || routeKey == "" {
			continue
		}
		out[flowID] = routeKey
	}
	return out
}

func normalizeCIDR(cidr string) string {
	if strings.Contains(cidr, "/") {
		return cidr
	}
	return cidr + "/32"
}
