package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livekit/sip/pkg/customsip"
	"github.com/stretchr/testify/require"
)

func TestLoadFlowIDLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flow_id_lookup.yaml")
	require.NoError(t, os.WriteFile(path, []byte("SS_INACTIVITY: \"8031136800\"\n"), 0o644))

	t.Setenv("SIP_FLOW_ID_LOOKUP_FILE", path)

	conf := &Config{}
	require.NoError(t, conf.loadFlowIDLookup())

	number, ok := conf.LookupFlowIDNumber("10.0.0.1", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031136800", number)
}

func TestLookupFlowIDNumber(t *testing.T) {
	t.Parallel()

	conf := &Config{
		FlowIDLookup: map[string]string{
			"SS_INACTIVITY": "8031136800",
		},
	}

	number, ok := conf.LookupFlowIDNumber("10.0.0.1", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031136800", number)

	number, ok = conf.LookupFlowIDNumber("10.0.0.1", "missing_flow")
	require.False(t, ok)
	require.Empty(t, number)
}

func TestLookupFlowIDNumberPrefersDirectRoutes(t *testing.T) {
	t.Parallel()

	conf := &Config{
		FlowIDLookup: map[string]string{
			"SS_INACTIVITY": "8030000000",
		},
		DirectRoutesEnabled: true,
	}
	conf.InitDirectRoutes()
	conf.DirectRoutes.ReplaceAll(customsip.Config{
		Default: &customsip.SourceConfig{
			Routes: map[string]string{"SS_INACTIVITY": "8031136800"},
		},
	})

	number, ok := conf.LookupFlowIDNumber("10.0.0.1", "SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "8031136800", number)
}

func TestLookupFlowIDNumberSourceScoped(t *testing.T) {
	t.Parallel()

	conf := &Config{
		DirectRoutesEnabled: true,
	}
	conf.InitDirectRoutes()
	conf.DirectRoutes.ReplaceAll(customsip.Config{
		Sources: map[string]customsip.SourceConfig{
			"10.150.115.130/32": {
				Routes: map[string]string{"flow_a": "8031111111"},
			},
		},
		Default: &customsip.SourceConfig{
			Routes: map[string]string{"flow_a": "8039999999"},
		},
	})

	number, ok := conf.LookupFlowIDNumber("10.150.115.130", "flow_a")
	require.True(t, ok)
	require.Equal(t, "8031111111", number)

	number, ok = conf.LookupFlowIDNumber("203.0.113.1", "flow_a")
	require.True(t, ok)
	require.Equal(t, "8039999999", number)
}
