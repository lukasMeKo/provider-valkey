// Package acluser implements the ACLUser managed resource reconciler.
package acluser

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/valkey-io/valkey-go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

	inner := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.ACLUserGroupVersionKind),
		managed.WithExternalConnector(&connector{
			kube:  mgr.GetClient(),
			usage: resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{}),
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.ACLUser{}).
		WithOptions(o.ForControllerRuntime()).
		Complete(&foregroundDeletionReconciler{
			inner:  inner,
			client: mgr.GetClient(),
		})
}

// foregroundDeletionReconciler strips the foregroundDeletion finalizer before
// delegating to the managed reconciler. Crossplane-runtime delays external
// deletion until len(finalizers) == 1, but the Kubernetes GC foregroundDeletion
// finalizer creates a deadlock for namespace-scoped managed resources with no
// dependents. Removing it is safe because ACLUser owns no child resources.
type foregroundDeletionReconciler struct {
	inner  reconcile.Reconciler
	client client.Client
}

func (r *foregroundDeletionReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	cr := &v1alpha1.ACLUser{}
	if err := r.client.Get(ctx, req.NamespacedName, cr); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Strip foregroundDeletion so the managed reconciler's delete path proceeds.
	if cr.DeletionTimestamp != nil && controllerutil.RemoveFinalizer(cr, "foregroundDeletion") {
		if err := r.client.Update(ctx, cr); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot remove foregroundDeletion finalizer: %w", err)
		}
	}

	return r.inner.Reconcile(ctx, req)
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
		return nil, fmt.Errorf("%s: %w", errTrackUsage, err)
	}

	// fetch ProviderConfig
	pc := &v1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, fmt.Errorf("%s: %w", errGetPC, err)
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
		return nil, fmt.Errorf("%s: %w", errGetSecret, err)
	}

	// resolve TLS setting
	useTLS := pc.Spec.Credentials.TLS != nil && pc.Spec.Credentials.TLS.Enabled

	vkClient, err := vkclient.NewClient(secret.Data, useTLS)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errNewClient, err)
	}

	return &external{
		client: vkClient,
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
	if errors.Is(err, vkclient.ErrUserNotFound) {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	if err != nil {
		return managed.ExternalObservation{}, fmt.Errorf("%s: %w", errObserve, err)
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
		return managed.ExternalCreation{}, fmt.Errorf("%s: %w", errGenPassword, err)
	}

	rules := buildRules(cr.Spec.ForProvider, password)

	if err := vkclient.SetACLUser(ctx, e.client, username, rules); err != nil {
		return managed.ExternalCreation{}, fmt.Errorf("%s: %w", errCreate, err)
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
	rules := buildRulesWithoutPassword(cr.Spec.ForProvider)

	if err := vkclient.SetACLUser(ctx, e.client, username, rules); err != nil {
		return managed.ExternalUpdate{}, fmt.Errorf("%s: %w", errUpdate, err)
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
		return managed.ExternalDelete{}, fmt.Errorf("%s: %w", errDelete, err)
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

	if p.Enabled != nil && !*p.Enabled {
		rules = append(rules, "off")
	} else {
		rules = append(rules, "on")
	}

	rules = append(rules, ">"+password)
	rules = append(rules, p.Rules...)

	return rules
}

// buildRulesWithoutPassword constructs rules for update without changing the password.
// Uses resetkeys/resetchannels/nocommands to clear permission state while preserving passwords.
func buildRulesWithoutPassword(p v1alpha1.ACLUserParameters) []string {
	rules := []string{"resetkeys", "resetchannels", "nocommands"}

	if p.Enabled != nil && !*p.Enabled {
		rules = append(rules, "off")
	} else {
		rules = append(rules, "on")
	}

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

	// extract desired keys, commands, and channels from user rules
	desiredKeys, desiredCmds, desiredChannels := classifyRules(p.Rules)

	// compare key patterns
	if !patternsMatch(desiredKeys, info.Keys) {
		return false
	}

	// compare commands (set-based, order-independent)
	if !commandsMatch(desiredCmds, info.Commands) {
		return false
	}

	// compare channel patterns
	if !patternsMatch(desiredChannels, info.Channels) {
		return false
	}

	return true
}

// classifyRules separates user-defined rules into keys, commands, and channels.
func classifyRules(rules []string) (keys, commands, channels []string) {
	for _, r := range rules {
		switch {
		case strings.HasPrefix(r, "~") || strings.HasPrefix(r, "%"):
			keys = append(keys, r)
		case r == "allkeys":
			keys = append(keys, "~*")
		case strings.HasPrefix(r, "&"):
			channels = append(channels, r)
		case r == "allchannels":
			channels = append(channels, "&*")
		case strings.HasPrefix(r, "+") || strings.HasPrefix(r, "-") ||
			r == "allcommands" || r == "nocommands":
			commands = append(commands, r)
		}
	}
	return keys, commands, channels
}

// patternsMatch compares desired patterns against the observed string from Valkey.
// An empty desired set means no patterns should exist (post-reset state).
func patternsMatch(desired []string, observed string) bool {
	expected := strings.Join(desired, " ")
	return strings.TrimSpace(expected) == strings.TrimSpace(observed)
}

// commandsMatch compares desired command rules against observed commands.
// Both sides are split into tokens and compared as sorted sets for order independence.
func commandsMatch(desired []string, observed string) bool {
	// normalize desired: expand aliases
	var normalizedDesired []string
	for _, c := range desired {
		switch c {
		case "allcommands":
			normalizedDesired = append(normalizedDesired, "+@all")
		case "nocommands":
			normalizedDesired = append(normalizedDesired, "-@all")
		default:
			normalizedDesired = append(normalizedDesired, c)
		}
	}

	// split observed into tokens
	observedTokens := splitTokens(observed)

	// empty desired means no commands (post-reset state = "-@all")
	if len(normalizedDesired) == 0 {
		return len(observedTokens) == 0 || (len(observedTokens) == 1 && observedTokens[0] == "-@all")
	}

	sort.Strings(normalizedDesired)
	sort.Strings(observedTokens)

	if len(normalizedDesired) != len(observedTokens) {
		return false
	}
	for i := range normalizedDesired {
		if normalizedDesired[i] != observedTokens[i] {
			return false
		}
	}
	return true
}

// splitTokens splits a space-separated string into non-empty tokens.
func splitTokens(s string) []string {
	var tokens []string
	for _, t := range strings.Fields(s) {
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
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
