package customsip

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsDefaultFlowExtractor(t *testing.T) {
	t.Parallel()
	require.True(t, IsDefaultFlowExtractor(DefaultFlowExtractor))
	require.True(t, IsDefaultFlowExtractor(FlowExtractor{}))
	require.True(t, IsDefaultFlowExtractor(FlowExtractor{
		Header: "User-to-User", Format: "uuid_pipe", Encoding: "auto",
	}))
	require.False(t, IsDefaultFlowExtractor(FlowExtractor{
		Header: "X-Flow-Id", Format: "plain",
	}))
}

// Legacy voice-agent pushes a flat flow_id → route_key map with no extractor config.
func TestBackwardCompatLegacyFlatPUT(t *testing.T) {
	t.Parallel()
	cfg, err := DecodeConfigJSON(strings.NewReader(`{"SS_INACTIVITY":"8031136800"}`))
	require.NoError(t, err)
	require.Equal(t, "8031136800", cfg.Default.Routes["SS_INACTIVITY"])
	require.Empty(t, cfg.Sources)

	store := NewStore()
	cfg.Apply(store)

	require.Equal(t, DefaultFlowExtractor, store.LookupExtractor("10.150.115.130"))
	_, flowID, ok := ParseFlowID(store.LookupExtractor("10.150.115.130"), "uuid|SS_INACTIVITY")
	require.True(t, ok)
	require.Equal(t, "SS_INACTIVITY", flowID)
	require.Equal(t, "8031136800", mustLookupRoute(t, store, "10.150.115.130", "SS_INACTIVITY"))
}

func mustLookupRoute(t *testing.T, store *Store, sourceIP, flowID string) string {
	t.Helper()
	v, ok := store.LookupRoute(sourceIP, flowID)
	require.True(t, ok)
	return v
}
