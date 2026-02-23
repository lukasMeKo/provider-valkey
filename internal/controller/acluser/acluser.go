// Package acluser implements the ACLUser managed resource reconciler.
package acluser

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/pkg/errors"
	"github.com/valkey-io/valkey-go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/lukasMeko/provider-valkey/apis/v1alpha1"
	vkclient "github.com/lukasMeko/provider-valkey/internal/clients"
)

const (
	errNotACLUser  = "managed resource is not an ACLUser"
	errTrackUsage  = "cannot track ProviderConfig usage"
	errGetPC       = "cannot get ProviderConfig"
	errGetSecret   = "cannot get credentials secret"
	errNewClient   = "cannot create Valkey client"
	errObserve     = "cannot observe ACL user"
	errCreate      = "cannot create ACL user"
	errUpdate      = "cannot update ACL user"
	errDelete      = "cannot delete ACL user"
	errGenPassword = "cannot generate password"
	passwordLength = 32
)

// Setup adds a controller that reconciles ACLUser managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.ACLUserGroupKind)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.ACLUser{}).
		WithOptions(o.ForControllerRuntime()).
		Complete(managed.NewReconciler(mgr,
			resource.ManagedKind(v1alpha1.ACLUserGroupVersionKind),
			managed.WithExternalConnector(&connector{
				kube:  mgr.GetClient(),
				usage: resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{}),
			}),
			managed.WithLogger(o.Logger.WithValues("controller", name)),
			managed.WithPollInterval(o.PollInterval),
			managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		))
}

type connector struct {
	kube  client.Client
	usage *resource.ProviderConfigUsageTracker
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.ACLUser)
	if !ok {
		return nil, errors.New(errNotACLUser)
	}

	if err := c.usage.Track(ctx, cr); err != nil {
		return nil, errors.Wrap(err, errTrackUsage)
	}

	// fetch ProviderConfig
	pc := &v1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	// fetch credentials secret
	ref := pc.Spec.Credentials.SecretRef
	if ref == nil {
		return nil, errors.New("credentials secretRef is required")
	}
	secret := &corev1.Secret{}
	if err := c.kube.Get(ctx, types.NamespacedName{
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}, secret); err != nil {
		return nil, errors.Wrap(err, errGetSecret)
	}

	client, err := vkclient.NewClient(ctx, secret.Data)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}

	return &external{
		client: client,
		kube:   c.kube,
		creds:  secret.Data,
	}, nil
}

type external struct {
	client valkey.Client
	kube   client.Client
	creds  map[string][]byte
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.ACLUser)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotACLUser)
	}

	username := meta.GetExternalName(cr)
	info, err := vkclient.GetACLUser(ctx, e.client, username)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errObserve)
	}

	// user doesn't exist
	if info == nil {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}

	// populate observed state
	cr.Status.AtProvider = v1alpha1.ACLUserObservation{
		Flags:    info.Flags,
		Commands: info.Commands,
		Keys:     info.Keys,
		Channels: info.Channels,
	}
	cr.SetConditions(xpv1.Available())

	upToDate := isUpToDate(cr.Spec.ForProvider, info)

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: upToDate,
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.ACLUser)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotACLUser)
	}

	username := meta.GetExternalName(cr)

	// generate random password
	password, err := generatePassword()
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errGenPassword)
	}

	// build rules: reset, on/off, password, user rules
	rules := buildRules(cr.Spec.ForProvider, password)

	if err := vkclient.SetACLUser(ctx, e.client, username, rules); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreate)
	}

	// publish connection details
	endpoint := string(e.creds[vkclient.KeyEndpoint])
	port := string(e.creds[vkclient.KeyPort])
	if port == "" {
		port = "6379"
	}

	return managed.ExternalCreation{
		ConnectionDetails: managed.ConnectionDetails{
			xpv1.ResourceCredentialsSecretUserKey:     []byte(username),
			xpv1.ResourceCredentialsSecretPasswordKey: []byte(password),
			xpv1.ResourceCredentialsSecretEndpointKey: []byte(endpoint),
			xpv1.ResourceCredentialsSecretPortKey:     []byte(port),
		},
	}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.ACLUser)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotACLUser)
	}

	username := meta.GetExternalName(cr)

	// rebuild rules without password (keep existing password)
	rules := buildRulesWithoutPassword(cr.Spec.ForProvider)

	if err := vkclient.SetACLUser(ctx, e.client, username, rules); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdate)
	}

	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr, ok := mg.(*v1alpha1.ACLUser)
	if !ok {
		return managed.ExternalDelete{}, errors.New(errNotACLUser)
	}

	username := meta.GetExternalName(cr)

	if err := vkclient.DeleteACLUser(ctx, e.client, username); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDelete)
	}

	return managed.ExternalDelete{}, nil
}

func (e *external) Disconnect(_ context.Context) error {
	e.client.Close()
	return nil
}

// buildRules constructs the full ACL SETUSER rule list including reset, state, and password.
func buildRules(p v1alpha1.ACLUserParameters, password string) []string {
	rules := []string{"reset"}

	// on/off
	if p.Enabled != nil && !*p.Enabled {
		rules = append(rules, "off")
	} else {
		rules = append(rules, "on")
	}

	// password
	rules = append(rules, ">"+password)

	// user-defined rules
	rules = append(rules, p.Rules...)

	return rules
}

// buildRulesWithoutPassword constructs rules for update (reset + state + user rules, no password change).
// We need to preserve the existing password, so we reset keys/commands/channels but re-add the password
// from the existing user by not including resetpass.
func buildRulesWithoutPassword(p v1alpha1.ACLUserParameters) []string {
	// Use resetkeys, resetchannels, and nocommands to clear permission state,
	// but don't use "reset" which would also clear passwords.
	rules := []string{"resetkeys", "resetchannels", "nocommands"}

	// on/off
	if p.Enabled != nil && !*p.Enabled {
		rules = append(rules, "off")
	} else {
		rules = append(rules, "on")
	}

	// user-defined rules
	rules = append(rules, p.Rules...)

	return rules
}

// isUpToDate checks whether the observed ACL user matches the desired state.
func isUpToDate(p v1alpha1.ACLUserParameters, info *vkclient.ACLUserInfo) bool {
	// check enabled state
	desiredEnabled := p.Enabled == nil || *p.Enabled
	observedEnabled := containsFlag(info.Flags, "on")
	if desiredEnabled != observedEnabled {
		return false
	}

	// check rules by rebuilding what the user should look like and comparing
	// with what's observed. This is a simplified comparison — we check that
	// the desired rules are reflected in the observed state.
	desiredRules := buildRulesWithoutPassword(p)
	// Apply the rules to determine expected state and compare
	// For now, use a simple heuristic: check key patterns, commands, channels
	expectedKeys, expectedCmds, expectedChannels := parseExpectedState(desiredRules)

	if expectedKeys != "" && expectedKeys != info.Keys {
		return false
	}
	if expectedCmds != "" && !commandsMatch(expectedCmds, info.Commands) {
		return false
	}
	if expectedChannels != "" && expectedChannels != info.Channels {
		return false
	}

	return true
}

// parseExpectedState extracts expected keys, commands, and channels from rules.
func parseExpectedState(rules []string) (keys, commands, channels string) {
	var keyParts, cmdParts, chanParts []string
	for _, r := range rules {
		switch {
		case strings.HasPrefix(r, "~") || strings.HasPrefix(r, "%"):
			keyParts = append(keyParts, r)
		case r == "allkeys":
			keyParts = append(keyParts, "~*")
		case strings.HasPrefix(r, "&"):
			chanParts = append(chanParts, r)
		case r == "allchannels":
			chanParts = append(chanParts, "&*")
		case strings.HasPrefix(r, "+") || strings.HasPrefix(r, "-") ||
			r == "allcommands" || r == "nocommands":
			cmdParts = append(cmdParts, r)
		}
	}
	return strings.Join(keyParts, " "), strings.Join(cmdParts, " "), strings.Join(chanParts, " ")
}

// commandsMatch does a normalized comparison of command strings.
func commandsMatch(expected, observed string) bool {
	// normalize by trimming and comparing
	return strings.TrimSpace(expected) == strings.TrimSpace(observed)
}

func containsFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

func generatePassword() (string, error) {
	b := make([]byte, passwordLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
