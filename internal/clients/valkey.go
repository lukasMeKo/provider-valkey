// Package clients provides Valkey client utilities for the provider.
package clients

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	"github.com/valkey-io/valkey-go"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
)

// Standard credential keys from crossplane-runtime.
const (
	KeyEndpoint = xpv1.ResourceCredentialsSecretEndpointKey
	KeyPort     = xpv1.ResourceCredentialsSecretPortKey
	KeyUsername = xpv1.ResourceCredentialsSecretUserKey
	KeyPassword = xpv1.ResourceCredentialsSecretPasswordKey
)

// ACLUserInfo holds the parsed result of ACL GETUSER.
type ACLUserInfo struct {
	Flags    []string
	Commands string
	Keys     string
	Channels string
}

// NewClient creates a new Valkey client from connection secret data.
func NewClient(creds map[string][]byte, useTLS bool) (valkey.Client, error) {
	endpoint := string(creds[KeyEndpoint])
	port := string(creds[KeyPort])
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required in credentials secret")
	}
	if port == "" {
		port = "6379"
	}

	opts := valkey.ClientOption{
		InitAddress: []string{net.JoinHostPort(endpoint, port)},
	}
	if u := string(creds[KeyUsername]); u != "" {
		opts.Username = u
	}
	if p := string(creds[KeyPassword]); p != "" {
		opts.Password = p
	}
	if useTLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("cannot create valkey client: %w", err)
	}
	return client, nil
}

// GetACLUser retrieves ACL information for a user. Returns nil if the user
// does not exist.
func GetACLUser(ctx context.Context, c valkey.Client, username string) (*ACLUserInfo, error) {
	resp := c.Do(ctx, c.B().AclGetuser().Username(username).Build())
	if err := resp.Error(); err != nil {
		if isUserNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ACL GETUSER failed: %w", err)
	}

	m, err := resp.AsMap()
	if err != nil {
		return nil, fmt.Errorf("cannot parse ACL GETUSER response as map: %w", err)
	}

	info := &ACLUserInfo{}

	if flags, ok := m["flags"]; ok {
		sl, err := flags.AsStrSlice()
		if err != nil {
			return nil, fmt.Errorf("cannot parse flags: %w", err)
		}
		info.Flags = sl
	}
	if cmds, ok := m["commands"]; ok {
		s, err := cmds.ToString()
		if err != nil {
			return nil, fmt.Errorf("cannot parse commands: %w", err)
		}
		info.Commands = s
	}
	if keys, ok := m["keys"]; ok {
		s, err := keys.ToString()
		if err != nil {
			return nil, fmt.Errorf("cannot parse keys: %w", err)
		}
		info.Keys = s
	}
	if channels, ok := m["channels"]; ok {
		s, err := channels.ToString()
		if err != nil {
			return nil, fmt.Errorf("cannot parse channels: %w", err)
		}
		info.Channels = s
	}

	return info, nil
}

// SetACLUser creates or updates an ACL user with the given rules.
// The rules slice should contain ACL rule tokens (e.g. "on", ">password", "~key:*", "+@read").
func SetACLUser(ctx context.Context, c valkey.Client, username string, rules []string) error {
	args := make([]string, 0, 3+len(rules))
	args = append(args, "ACL", "SETUSER", username)
	args = append(args, rules...)

	cmd := c.B().Arbitrary(args...).Build()
	if err := c.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("ACL SETUSER %s failed: %w", username, err)
	}
	return nil
}

// DeleteACLUser removes an ACL user.
func DeleteACLUser(ctx context.Context, c valkey.Client, username string) error {
	if err := c.Do(ctx, c.B().AclDeluser().Username(username).Build()).Error(); err != nil {
		return fmt.Errorf("ACL DELUSER %s failed: %w", username, err)
	}
	return nil
}

func isUserNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "not found")
}
