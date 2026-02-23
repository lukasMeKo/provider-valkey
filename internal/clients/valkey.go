// Package clients provides Valkey client utilities for the provider.
package clients

import (
	"context"
	"fmt"
	"net"

	"github.com/pkg/errors"
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
func NewClient(ctx context.Context, creds map[string][]byte) (valkey.Client, error) {
	endpoint := string(creds[KeyEndpoint])
	port := string(creds[KeyPort])
	if endpoint == "" {
		return nil, errors.New("endpoint is required in credentials secret")
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

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create valkey client")
	}
	return client, nil
}

// GetACLUser retrieves ACL information for a user. Returns nil if the user
// does not exist.
func GetACLUser(ctx context.Context, c valkey.Client, username string) (*ACLUserInfo, error) {
	resp := c.Do(ctx, c.B().AclGetuser().Username(username).Build())
	if err := resp.Error(); err != nil {
		// Valkey returns an error when user doesn't exist
		if isUserNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "ACL GETUSER failed")
	}

	m, err := resp.AsMap()
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse ACL GETUSER response as map")
	}

	info := &ACLUserInfo{}

	if flags, ok := m["flags"]; ok {
		info.Flags, _ = flags.AsStrSlice()
	}
	if cmds, ok := m["commands"]; ok {
		info.Commands, _ = cmds.ToString()
	}
	if keys, ok := m["keys"]; ok {
		info.Keys, _ = keys.ToString()
	}
	if channels, ok := m["channels"]; ok {
		info.Channels, _ = channels.ToString()
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
		return errors.Wrap(err, fmt.Sprintf("ACL SETUSER %s failed", username))
	}
	return nil
}

// DeleteACLUser removes an ACL user.
func DeleteACLUser(ctx context.Context, c valkey.Client, username string) error {
	if err := c.Do(ctx, c.B().AclDeluser().Username(username).Build()).Error(); err != nil {
		return errors.Wrap(err, fmt.Sprintf("ACL DELUSER %s failed", username))
	}
	return nil
}

func isUserNotFound(err error) bool {
	if err == nil {
		return false
	}
	// Valkey returns "ERR User ... doesn't exist" or similar
	msg := err.Error()
	return contains(msg, "doesn't exist") || contains(msg, "does not exist") || contains(msg, "not found")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
