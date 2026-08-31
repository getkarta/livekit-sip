package customsip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeConfigJSONLegacyFlat(t *testing.T) {
	t.Parallel()
	cfg, err := DecodeConfigJSON(strings.NewReader(`{"SS_INACTIVITY":"8031111111"}`))
	require.NoError(t, err)
	require.NotNil(t, cfg.Default)
	require.Equal(t, map[string]string{"SS_INACTIVITY": "8031111111"}, cfg.Default.Routes)
	require.Empty(t, cfg.Sources)
}

func TestDecodeConfigJSONLegacyWrapped(t *testing.T) {
	t.Parallel()
	body := `{
		"routes": {"Onb_VoiceBot": "8036291847"},
		"default_extractor": {"header": "User-to-User", "format": "uuid_pipe", "encoding": "auto"},
		"extractors_by_source": {
			"10.150.115.130/32": {"header": "User-to-User", "format": "uuid_pipe", "encoding": "auto"}
		}
	}`
	cfg, err := DecodeConfigJSON(strings.NewReader(body))
	require.NoError(t, err)
	require.NotNil(t, cfg.Default)
	require.Equal(t, "8036291847", cfg.Default.Routes["Onb_VoiceBot"])
	require.Equal(t, "User-to-User", cfg.Default.Extractor.Header)
	require.Equal(t, "uuid_pipe", cfg.Sources["10.150.115.130/32"].Extractor.Format)
}

func TestDecodeConfigJSONDefaultOnly(t *testing.T) {
	t.Parallel()
	body := `{
		"default": {
			"extractor": {"header": "User-to-User", "format": "uuid_pipe", "encoding": "auto"},
			"routes": {"SS_INACTIVITY": "8031136800"}
		}
	}`
	cfg, err := DecodeConfigJSON(strings.NewReader(body))
	require.NoError(t, err)
	require.Empty(t, cfg.Sources)
	require.NotNil(t, cfg.Default)
	require.Equal(t, "8031136800", cfg.Default.Routes["SS_INACTIVITY"])
	require.Equal(t, "User-to-User", cfg.Default.Extractor.Header)
}

func TestDecodeConfigJSONSources(t *testing.T) {
	t.Parallel()
	body := `{
		"sources": {
			"10.150.115.130/32": {
				"extractor": {"header": "User-to-User", "format": "uuid_pipe", "encoding": "auto"},
				"routes": {"Onb_VoiceBot": "8036291847", "SETTLEMENT_INBOUND": "8038472916"}
			},
			"203.0.113.50/32": {
				"extractor": {"header": "X-Flow-Id", "format": "plain", "encoding": "none"},
				"routes": {"billing": "8032222222"}
			}
		},
		"default": {
			"extractor": {"header": "User-to-User", "format": "uuid_pipe", "encoding": "auto"},
			"routes": {"SS_INACTIVITY": "8031136800"}
		}
	}`
	cfg, err := DecodeConfigJSON(strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, "8036291847", cfg.Sources["10.150.115.130/32"].Routes["Onb_VoiceBot"])
	require.Equal(t, "X-Flow-Id", cfg.Sources["203.0.113.50/32"].Extractor.Header)
	require.Equal(t, "8031136800", cfg.Default.Routes["SS_INACTIVITY"])
}

func TestConfigApplyAndEncodeLegacyGET(t *testing.T) {
	t.Parallel()
	store := NewStore()
	cfg := Config{Default: &SourceConfig{Routes: map[string]string{"A": "8030000001"}}}
	cfg.Apply(store)

	var buf bytes.Buffer
	require.NoError(t, EncodeConfigJSON(&buf, store))
	require.Equal(t, `{"A":"8030000001"}`+"\n", buf.String())
}

func TestConfigApplyAndEncodeSourcesGET(t *testing.T) {
	t.Parallel()
	store := NewStore()
	cfg := Config{
		Sources: map[string]SourceConfig{
			"10.150.115.130/32": {
				Extractor: FlowExtractor{Header: "User-to-User", Format: "uuid_pipe", Encoding: "auto"},
				Routes:    map[string]string{"Onb_VoiceBot": "8036291847"},
			},
		},
	}
	cfg.Apply(store)

	var buf bytes.Buffer
	require.NoError(t, EncodeConfigJSON(&buf, store))
	out := buf.String()
	require.Contains(t, out, `"sources"`)
	require.Contains(t, out, "10.150.115.130/32")
	require.Contains(t, out, "Onb_VoiceBot")
}
