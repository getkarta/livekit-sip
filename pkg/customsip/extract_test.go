package customsip

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseFlowIDUUIDPipe(t *testing.T) {
	t.Parallel()
	extractor := FlowExtractor{
		Header:   "User-to-User",
		Format:   "uuid_pipe",
		Encoding: "auto",
	}

	plain := "550e8400-e29b-41d4-a716-446655440000|SS_INACTIVITY"
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))

	tests := []struct {
		name      string
		value     string
		wantFlow  string
		wantOK    bool
		wantSess  string
	}{
		{
			name:     "plain pipe separated",
			value:    plain,
			wantFlow: "SS_INACTIVITY",
			wantOK:   true,
			wantSess: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "base64 with encoding param",
			value:    encoded + ";encoding=base64",
			wantFlow: "SS_INACTIVITY",
			wantOK:   true,
		},
		{
			name:     "base64 without encoding param",
			value:    encoded,
			wantFlow: "SS_INACTIVITY",
			wantOK:   true,
		},
		{
			name:   "missing pipe",
			value:  "no-flow-id",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID, flowID, ok := ParseFlowID(extractor, tt.value)
			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			require.Equal(t, tt.wantFlow, flowID)
			if tt.wantSess != "" {
				require.Equal(t, tt.wantSess, sessionID)
			}
		})
	}
}

func TestParseFlowIDPlain(t *testing.T) {
	t.Parallel()
	extractor := FlowExtractor{
		Header: "X-Flow-Id",
		Format: "plain",
	}
	_, flowID, ok := ParseFlowID(extractor, "  billing_flow  ")
	require.True(t, ok)
	require.Equal(t, "billing_flow", flowID)
}

func TestDefaultFlowExtractorFallback(t *testing.T) {
	t.Parallel()
	plain := "uuid|MY_FLOW"
	_, flowID, ok := ParseFlowID(FlowExtractor{}, plain)
	require.True(t, ok)
	require.Equal(t, "MY_FLOW", flowID)
}
