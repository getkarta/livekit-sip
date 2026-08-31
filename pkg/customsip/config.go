package customsip

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type sourceEntryJSON struct {
	Extractor *FlowExtractor  `json:"extractor,omitempty"`
	Routes    map[string]string `json:"routes,omitempty"`
}

type configSnapshotJSON struct {
	Sources map[string]sourceEntryJSON `json:"sources,omitempty"`
	Default *sourceEntryJSON           `json:"default,omitempty"`
}

// Legacy wrapped format kept for backward compatibility with older voice-agent pushes.
type legacyWrappedJSON struct {
	Routes             map[string]string        `json:"routes"`
	DefaultExtractor   *FlowExtractor           `json:"default_extractor,omitempty"`
	ExtractorsBySource map[string]FlowExtractor `json:"extractors_by_source,omitempty"`
}

// DecodeConfigJSON parses custom-SIP config from voice-agent.
// Accepts legacy flat {flow_id: route_key}, old wrapped {routes, extractors_by_source},
// or nested {sources, default}.
func DecodeConfigJSON(r io.Reader) (Config, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("invalid JSON body: %w", err)
	}
	if len(raw) == 0 {
		return Config{Sources: map[string]SourceConfig{}}, nil
	}

	switch {
	case hasKey(raw, "sources") || hasNestedDefault(raw):
		return decodeSourcesConfig(raw)
	case hasKey(raw, "routes"):
		return decodeLegacyWrappedConfig(raw)
	default:
		routes, err := parseRouteMapRaw(raw)
		if err != nil {
			return Config{}, err
		}
		if len(routes) == 0 {
			return Config{Sources: map[string]SourceConfig{}}, nil
		}
		return Config{
			Sources: map[string]SourceConfig{},
			Default: &SourceConfig{Routes: routes},
		}, nil
	}
}

func hasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

// hasNestedDefault reports whether "default" is a nested bucket object (not a flat route value).
func hasNestedDefault(raw map[string]json.RawMessage) bool {
	msg, ok := raw["default"]
	if !ok {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(msg, &obj) == nil
}

func decodeSourcesConfig(raw map[string]json.RawMessage) (Config, error) {
	cfg := Config{Sources: map[string]SourceConfig{}}
	if msg, ok := raw["sources"]; ok {
		var sources map[string]sourceEntryJSON
		if err := json.Unmarshal(msg, &sources); err != nil {
			return Config{}, fmt.Errorf("invalid sources object: %w", err)
		}
		for cidr, entry := range sources {
			src, err := sourceConfigFromJSON(entry)
			if err != nil {
				return Config{}, fmt.Errorf("invalid source %q: %w", cidr, err)
			}
			if src.Routes != nil || src.Extractor.valid() {
				cfg.Sources[cidr] = src
			}
		}
	}
	if msg, ok := raw["default"]; ok {
		var entry sourceEntryJSON
		if err := json.Unmarshal(msg, &entry); err != nil {
			return Config{}, fmt.Errorf("invalid default object: %w", err)
		}
		src, err := sourceConfigFromJSON(entry)
		if err != nil {
			return Config{}, fmt.Errorf("invalid default: %w", err)
		}
		if src.Routes != nil || src.Extractor.valid() {
			cfg.Default = &src
		}
	}
	return cfg, nil
}

func decodeLegacyWrappedConfig(raw map[string]json.RawMessage) (Config, error) {
	var wrapped legacyWrappedJSON
	if err := json.Unmarshal(mustMarshalObject(raw), &wrapped); err != nil {
		return Config{}, fmt.Errorf("invalid wrapped config: %w", err)
	}

	cfg := Config{Sources: map[string]SourceConfig{}}
	defaultCfg := SourceConfig{Routes: map[string]string{}}
	if wrapped.Routes != nil {
		parsed, err := parseRouteMapAny(anyMapFromStrings(wrapped.Routes))
		if err != nil {
			return Config{}, err
		}
		defaultCfg.Routes = parsed
	}
	if wrapped.DefaultExtractor != nil {
		defaultCfg.Extractor = wrapped.DefaultExtractor.normalized()
	}
	if len(defaultCfg.Routes) > 0 || defaultCfg.Extractor.valid() {
		cfg.Default = &defaultCfg
	}

	for cidr, ext := range wrapped.ExtractorsBySource {
		cfg.Sources[cidr] = SourceConfig{Extractor: ext.normalized()}
	}
	return cfg, nil
}

func sourceConfigFromJSON(entry sourceEntryJSON) (SourceConfig, error) {
	out := SourceConfig{}
	if entry.Routes != nil {
		parsed, err := parseRouteMapAny(anyMapFromStrings(entry.Routes))
		if err != nil {
			return SourceConfig{}, err
		}
		out.Routes = parsed
	}
	if entry.Extractor != nil {
		out.Extractor = entry.Extractor.normalized()
	}
	return out, nil
}

func parseRouteMapRaw(raw map[string]json.RawMessage) (map[string]string, error) {
	anyMap := make(map[string]any, len(raw))
	for k, v := range raw {
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return nil, fmt.Errorf("invalid route value for %q: %w", k, err)
		}
		anyMap[k] = val
	}
	return parseRouteMapAny(anyMap)
}

func parseRouteMapAny(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("route value for %q must be a string", k)
		}
		if strings.TrimSpace(k) == "" || strings.TrimSpace(s) == "" {
			continue
		}
		out[k] = s
	}
	return out, nil
}

// EncodeConfigJSON writes the config snapshot for GET responses.
func EncodeConfigJSON(w io.Writer, store *Store) error {
	if store == nil {
		return json.NewEncoder(w).Encode(map[string]string{})
	}
	cfg := store.Snapshot()
	if isLegacyFlatOnly(cfg) {
		return json.NewEncoder(w).Encode(cfg.Default.Routes)
	}
	return json.NewEncoder(w).Encode(toSnapshotJSON(cfg))
}

func isLegacyFlatOnly(cfg Config) bool {
	if len(cfg.Sources) > 0 {
		return false
	}
	if cfg.Default == nil || len(cfg.Default.Routes) == 0 {
		return false
	}
	return !cfg.Default.Extractor.valid() || IsDefaultFlowExtractor(cfg.Default.Extractor)
}

func toSnapshotJSON(cfg Config) configSnapshotJSON {
	out := configSnapshotJSON{}
	if len(cfg.Sources) > 0 {
		out.Sources = make(map[string]sourceEntryJSON, len(cfg.Sources))
		for cidr, src := range cfg.Sources {
			out.Sources[cidr] = toSourceEntryJSON(src)
		}
	}
	if cfg.Default != nil && (len(cfg.Default.Routes) > 0 || cfg.Default.Extractor.valid()) {
		entry := toSourceEntryJSON(*cfg.Default)
		out.Default = &entry
	}
	return out
}

func toSourceEntryJSON(src SourceConfig) sourceEntryJSON {
	out := sourceEntryJSON{}
	if len(src.Routes) > 0 {
		out.Routes = src.Routes
	}
	if src.Extractor.valid() {
		ext := src.Extractor
		out.Extractor = &ext
	}
	return out
}

// Apply loads config into the store.
func (c Config) Apply(store *Store) {
	if store != nil {
		store.ReplaceAll(c)
	}
}

func mustMarshalObject(raw map[string]json.RawMessage) []byte {
	anyMap := make(map[string]any, len(raw))
	for k, v := range raw {
		var val any
		_ = json.Unmarshal(v, &val)
		anyMap[k] = val
	}
	b, _ := json.Marshal(anyMap)
	return b
}

func anyMapFromStrings(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
