package customsip

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const RoutesPath = "/private/custom-sip/routes"

// Handler serves live route-map updates for voice-agent (internal VPC only, no auth).
type Handler struct {
	Store     *Store
	OnReplace func(size int, err error)
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc(RoutesPath, h.handleRoutes)
}

func (h *Handler) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		h.putRoutes(w, r)
	case http.MethodGet:
		h.getRoutes(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) putRoutes(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	cfg, err := DecodeConfigJSON(r.Body)
	if err != nil {
		if h.OnReplace != nil {
			h.OnReplace(0, err)
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.Store == nil {
		http.Error(w, "custom-sip routes not enabled", http.StatusServiceUnavailable)
		return
	}
	cfg.Apply(h.Store)
	size := h.Store.Size()
	if h.OnReplace != nil {
		h.OnReplace(size, nil)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"size": size,
	})
}

func (h *Handler) getRoutes(w http.ResponseWriter, r *http.Request) {
	_ = r
	if h.Store == nil {
		http.Error(w, "custom-sip routes not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := EncodeConfigJSON(w, h.Store); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// DecodeRoutesJSON parses a JSON object of string→string route mappings.
// Deprecated: use DecodeConfigJSON for new callers.
func DecodeRoutesJSON(r io.Reader) (map[string]string, error) {
	cfg, err := DecodeConfigJSON(r)
	if err != nil {
		return nil, err
	}
	if cfg.Default != nil {
		return cfg.Default.Routes, nil
	}
	return map[string]string{}, nil
}

// ValidateRouteValue is used by tests and legacy validation helpers.
func ValidateRouteValue(k string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("route value for %q must be a string", k)
	}
	if strings.TrimSpace(k) == "" || strings.TrimSpace(s) == "" {
		return "", nil
	}
	return s, nil
}
