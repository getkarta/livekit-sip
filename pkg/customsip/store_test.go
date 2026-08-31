package customsip

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreReplaceAndLookup(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{
		Default: &SourceConfig{Routes: map[string]string{
			"SS_INACTIVITY": "8031111111",
		}},
	})
	require.Equal(t, 1, store.Size())

	v, ok := store.LookupRoute("10.0.0.1", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031111111", v)

	_, ok = store.LookupRoute("10.0.0.1", "missing")
	require.False(t, ok)

	store.ReplaceAll(Config{})
	require.Equal(t, 0, store.Size())
}

func TestStoreSourceScopedRoutes(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{
		Sources: map[string]SourceConfig{
			"10.150.115.130/32": {
				Routes: map[string]string{"shared_flow": "8031111111"},
			},
			"203.0.113.50/32": {
				Routes: map[string]string{"shared_flow": "8032222222"},
			},
		},
	})

	v, ok := store.LookupRoute("10.150.115.130", "shared_flow")
	require.True(t, ok)
	require.Equal(t, "8031111111", v)

	v, ok = store.LookupRoute("203.0.113.50", "shared_flow")
	require.True(t, ok)
	require.Equal(t, "8032222222", v)
}

func TestStoreLookupExtractor(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{
		Sources: map[string]SourceConfig{
			"10.150.115.130/32": {
				Extractor: FlowExtractor{Header: "User-to-User", Format: "uuid_pipe", Encoding: "auto"},
			},
			"10.0.0.0/8": {
				Extractor: FlowExtractor{Header: "X-VPC", Format: "plain"},
			},
		},
		Default: &SourceConfig{
			Extractor: FlowExtractor{Header: "X-Default", Format: "plain"},
		},
	})

	ext := store.LookupExtractor("10.150.115.130")
	require.Equal(t, "User-to-User", ext.Header)

	ext = store.LookupExtractor("10.0.0.50")
	require.Equal(t, "X-VPC", ext.Header)

	ext = store.LookupExtractor("203.0.113.1")
	require.Equal(t, "X-Default", ext.Header)
}

func TestStoreNilUsesDefaultExtractor(t *testing.T) {
	t.Parallel()
	var store *Store
	ext := store.LookupExtractor("10.150.115.130")
	require.Equal(t, DefaultFlowExtractor, ext)
}

func TestStoreBareIPNormalizesTo32(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{
		Sources: map[string]SourceConfig{
			"10.150.115.130": {
				Extractor: FlowExtractor{Header: "User-to-User", Format: "uuid_pipe", Encoding: "auto"},
			},
		},
	})
	ext := store.LookupExtractor("10.150.115.130")
	require.Equal(t, "User-to-User", ext.Header)
}

func TestStoreDefaultRoutesFallback(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{
		Sources: map[string]SourceConfig{
			"10.150.115.130/32": {
				Extractor: FlowExtractor{Header: "User-to-User", Format: "uuid_pipe", Encoding: "auto"},
			},
		},
		Default: &SourceConfig{
			Routes: map[string]string{"SS_INACTIVITY": "8031136800"},
		},
	})

	v, ok := store.LookupRoute("10.150.115.130", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031136800", v)
}

func TestPrivateRoutesHTTP(t *testing.T) {
	t.Parallel()
	store := NewStore()
	store.ReplaceAll(Config{Default: &SourceConfig{Routes: map[string]string{"OLD": "8030000000"}}})

	h := &Handler{Store: store}
	mux := http.NewServeMux()
	h.Mount(mux)

	t.Run("get legacy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, RoutesPath, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "8030000000")
	})

	t.Run("put legacy flat", func(t *testing.T) {
		body := []byte(`{"SS_INACTIVITY":"8031136800","MF_LUMPSUM_VB":"8032222222"}`)
		req := httptest.NewRequest(http.MethodPut, RoutesPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), `"ok":true`)

		v, ok := store.LookupRoute("10.0.0.1", "SS_INACTIVITY")
		require.True(t, ok)
		require.Equal(t, "8031136800", v)
		_, ok = store.LookupRoute("10.0.0.1", "OLD")
		require.False(t, ok)
	})

	t.Run("put sources shape", func(t *testing.T) {
		body := []byte(`{
			"sources": {
				"10.150.115.130/32": {
					"extractor": {"header":"User-to-User","format":"uuid_pipe","encoding":"auto"},
					"routes": {"SS_INACTIVITY":"8031136800"}
				}
			}
		}`)
		req := httptest.NewRequest(http.MethodPut, RoutesPath, bytes.NewReader(body))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		ext := store.LookupExtractor("10.150.115.130")
		require.Equal(t, "User-to-User", ext.Header)

		req = httptest.NewRequest(http.MethodGet, RoutesPath, nil)
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Contains(t, rr.Body.String(), `"sources"`)
	})

	t.Run("put invalid value type", func(t *testing.T) {
		body := []byte(`{"SS_INACTIVITY":123}`)
		req := httptest.NewRequest(http.MethodPut, RoutesPath, bytes.NewReader(body))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestBootstrap(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/private/v1/custom-sip/routes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"SS_INACTIVITY":"8031136800"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	store := NewStore()
	size, err := Bootstrap(context.Background(), srv.URL+"/private/v1/custom-sip/routes", store, srv.Client())
	require.NoError(t, err)
	require.Equal(t, 1, size)
	v, ok := store.LookupRoute("10.0.0.1", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031136800", v)
}

func TestBootstrapSourcesConfig(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/private/v1/custom-sip/routes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"sources": {
				"10.150.115.130/32": {
					"extractor": {"header":"User-to-User","format":"uuid_pipe","encoding":"auto"},
					"routes": {"Onb_VoiceBot":"8036291847"}
				}
			}
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	store := NewStore()
	size, err := Bootstrap(context.Background(), srv.URL+"/private/v1/custom-sip/routes", store, srv.Client())
	require.NoError(t, err)
	require.Equal(t, 1, size)
	require.Equal(t, "User-to-User", store.LookupExtractor("10.150.115.130").Header)
	v, ok := store.LookupRoute("10.150.115.130", "Onb_VoiceBot")
	require.True(t, ok)
	require.Equal(t, "8036291847", v)
}
