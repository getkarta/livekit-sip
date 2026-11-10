package config

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOutboundNodeIPs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "empty", raw: "", want: nil},
		{name: "single", raw: `["65.1.49.79"]`, want: []string{"65.1.49.79"}},
		{name: "multiple", raw: `["65.1.49.79", "13.205.142.14"]`, want: []string{"65.1.49.79", "13.205.142.14"}},
		{name: "trims blanks", raw: `[" 65.1.49.79 ", "", "10.0.0.1"]`, want: []string{"65.1.49.79", "10.0.0.1"}},
		{name: "invalid json", raw: `not-json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOutboundNodeIPs(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAllowsOutbound(t *testing.T) {
	tests := []struct {
		name string
		conf *Config
		want bool
	}{
		{name: "nil config", conf: nil, want: true},
		{name: "empty list", conf: &Config{NAT1To1IP: "10.0.0.1"}, want: true},
		{
			name: "match",
			conf: &Config{NAT1To1IP: "65.1.49.79", OutboundNodeIPs: []string{"65.1.49.79"}},
			want: true,
		},
		{
			name: "no match",
			conf: &Config{NAT1To1IP: "10.0.0.196", OutboundNodeIPs: []string{"65.1.49.79"}},
			want: false,
		},
		{
			name: "match among multiple",
			conf: &Config{NAT1To1IP: "13.205.142.14", OutboundNodeIPs: []string{"65.1.49.79", "13.205.142.14"}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.conf.AllowsOutbound())
		})
	}
}

func TestLoadOutboundNodeIPsFromEnv(t *testing.T) {
	t.Setenv("SIP_OUTBOUND_NODE_IPS", `["65.1.49.79"]`)
	t.Setenv("ENV", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("SIP_OUTBOUND_NODE_IPS_SSM", "")

	c := &Config{NAT1To1IP: "65.1.49.79"}
	require.NoError(t, c.loadOutboundNodeIPs())
	assert.Equal(t, []string{"65.1.49.79"}, c.OutboundNodeIPs)
}

func TestLoadOutboundNodeIPsYamlWins(t *testing.T) {
	t.Setenv("SIP_OUTBOUND_NODE_IPS", `["1.1.1.1"]`)
	c := &Config{OutboundNodeIPs: []string{"65.1.49.79"}}
	require.NoError(t, c.loadOutboundNodeIPs())
	assert.Equal(t, []string{"65.1.49.79"}, c.OutboundNodeIPs)
}

func TestLoadOutboundNodeIPsFromSSM(t *testing.T) {
	prev := ssmGetParameter
	t.Cleanup(func() { ssmGetParameter = prev })

	ssmGetParameter = func(ctx context.Context, name string) (string, error) {
		assert.Equal(t, "/livekit/IndiaProd/sip/outbound_node_ips", name)
		return `["65.1.49.79"]`, nil
	}

	t.Setenv("SIP_OUTBOUND_NODE_IPS", "")
	t.Setenv("SIP_OUTBOUND_NODE_IPS_SSM", "")
	t.Setenv("ENV", "IndiaProd")
	_ = os.Unsetenv("ENVIRONMENT")

	c := &Config{}
	require.NoError(t, c.loadOutboundNodeIPs())
	assert.Equal(t, []string{"65.1.49.79"}, c.OutboundNodeIPs)
}

func TestLoadOutboundNodeIPsSSMPathOverride(t *testing.T) {
	prev := ssmGetParameter
	t.Cleanup(func() { ssmGetParameter = prev })

	ssmGetParameter = func(ctx context.Context, name string) (string, error) {
		assert.Equal(t, "/custom/path", name)
		return `["13.205.142.14"]`, nil
	}

	t.Setenv("SIP_OUTBOUND_NODE_IPS", "")
	t.Setenv("ENV", "IndiaStage")
	t.Setenv("SIP_OUTBOUND_NODE_IPS_SSM", "/custom/path")

	c := &Config{}
	require.NoError(t, c.loadOutboundNodeIPs())
	assert.Equal(t, []string{"13.205.142.14"}, c.OutboundNodeIPs)
}

func TestOutboundNodeIPsSSMPath(t *testing.T) {
	t.Setenv("SIP_OUTBOUND_NODE_IPS_SSM", "")
	t.Setenv("ENV", "IndiaStage")
	_ = os.Unsetenv("ENVIRONMENT")
	assert.Equal(t, "/livekit/IndiaStage/sip/outbound_node_ips", outboundNodeIPsSSMPath())

	t.Setenv("ENV", "")
	t.Setenv("ENVIRONMENT", "IndiaProd")
	assert.Equal(t, "/livekit/IndiaProd/sip/outbound_node_ips", outboundNodeIPsSSMPath())
}
