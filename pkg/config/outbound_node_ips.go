package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const (
	// DefaultOutboundNodeIPsSSMPathFmt is /livekit/{ENV}/sip/outbound_node_ips
	// e.g. /livekit/IndiaProd/sip/outbound_node_ips
	DefaultOutboundNodeIPsSSMPathFmt = "/livekit/%s/sip/outbound_node_ips"
)

// ssmGetParameter is swapped in tests.
var ssmGetParameter = getSSMParameter

func getSSMParameter(ctx context.Context, name string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}
	out, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", err
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("ssm parameter %q has empty value", name)
	}
	return *out.Parameter.Value, nil
}

func outboundNodeIPsEnvName() string {
	if v := strings.TrimSpace(os.Getenv("ENV")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("ENVIRONMENT"))
}

func outboundNodeIPsSSMPath() string {
	if p := strings.TrimSpace(os.Getenv("SIP_OUTBOUND_NODE_IPS_SSM")); p != "" {
		return p
	}
	env := outboundNodeIPsEnvName()
	if env == "" {
		return ""
	}
	return fmt.Sprintf(DefaultOutboundNodeIPsSSMPathFmt, env)
}

func parseOutboundNodeIPs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var ips []string
	if err := json.Unmarshal([]byte(raw), &ips); err != nil {
		return nil, fmt.Errorf("parse outbound_node_ips JSON: %w", err)
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			out = append(out, ip)
		}
	}
	return out, nil
}

// loadOutboundNodeIPs fills OutboundNodeIPs when not already set in yaml.
// Precedence:
//  1. yaml outbound_node_ips (already set)
//  2. SIP_OUTBOUND_NODE_IPS env (JSON array) — useful for local/tests
//  3. SSM: SIP_OUTBOUND_NODE_IPS_SSM, or /livekit/{ENV}/sip/outbound_node_ips
//
// If ENV (or SIP_OUTBOUND_NODE_IPS_SSM) is set, SSM load failures are fatal so
// outbound traffic cannot accidentally land on the wrong node.
func (c *Config) loadOutboundNodeIPs() error {
	if len(c.OutboundNodeIPs) > 0 {
		return nil
	}

	if raw := strings.TrimSpace(os.Getenv("SIP_OUTBOUND_NODE_IPS")); raw != "" {
		ips, err := parseOutboundNodeIPs(raw)
		if err != nil {
			return err
		}
		c.OutboundNodeIPs = ips
		return nil
	}

	path := outboundNodeIPsSSMPath()
	if path == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw, err := ssmGetParameter(ctx, path)
	if err != nil {
		return fmt.Errorf("load outbound_node_ips from SSM %q: %w", path, err)
	}
	ips, err := parseOutboundNodeIPs(raw)
	if err != nil {
		return fmt.Errorf("SSM %q: %w", path, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("SSM %q returned an empty outbound_node_ips list", path)
	}
	c.OutboundNodeIPs = ips
	return nil
}

// AllowsOutbound returns true when this node may handle CreateSIPParticipant jobs.
// Empty OutboundNodeIPs means no filter (any node may handle outbound).
func (c *Config) AllowsOutbound() bool {
	if c == nil || len(c.OutboundNodeIPs) == 0 {
		return true
	}
	myIP := c.NAT1To1IP
	for _, ip := range c.OutboundNodeIPs {
		if ip == myIP {
			return true
		}
	}
	return false
}
