package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpv2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
)

// ACLUserParameters define the desired state of a Valkey ACL user.
type ACLUserParameters struct {
	// Rules are ACL rule tokens applied to the user, e.g. ["~app:*", "+@read", "-@dangerous"].
	// These map directly to Valkey ACL SETUSER rule arguments.
	// Do not include on/off or password rules here; use the Enabled field
	// and let the provider manage passwords automatically.
	// +optional
	Rules []string `json:"rules,omitempty"`

	// Enabled controls whether the ACL user is active (can authenticate).
	// Defaults to true.
	// +optional
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`
}

// ACLUserObservation holds the observed state of the Valkey ACL user.
type ACLUserObservation struct {
	// Flags reported by ACL GETUSER (e.g. ["on"], ["off", "allkeys"]).
	// +optional
	Flags []string `json:"flags,omitempty"`

	// Commands is the command permission string (e.g. "+@all -@dangerous").
	// +optional
	Commands string `json:"commands,omitempty"`

	// Keys is the key pattern string (e.g. "~app:*").
	// +optional
	Keys string `json:"keys,omitempty"`

	// Channels is the channel pattern string (e.g. "&*").
	// +optional
	Channels string `json:"channels,omitempty"`
}

// ACLUserSpec defines the desired state of an ACLUser.
type ACLUserSpec struct {
	xpv2.ManagedResourceSpec `json:",inline"`
	ForProvider              ACLUserParameters `json:"forProvider"`
}

// ACLUserStatus represents the observed state of an ACLUser.
type ACLUserStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          ACLUserObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,valkey}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// ACLUser is a managed resource that represents a Valkey ACL user.
type ACLUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ACLUserSpec   `json:"spec"`
	Status ACLUserStatus `json:"status,omitempty"`
}

// GetCondition of this ACLUser.
func (u *ACLUser) GetCondition(ct xpv1.ConditionType) xpv1.Condition {
	return u.Status.GetCondition(ct)
}

// SetConditions of this ACLUser.
func (u *ACLUser) SetConditions(c ...xpv1.Condition) {
	u.Status.SetConditions(c...)
}

// GetManagementPolicies of this ACLUser.
func (u *ACLUser) GetManagementPolicies() xpv1.ManagementPolicies {
	return u.Spec.ManagementPolicies
}

// SetManagementPolicies of this ACLUser.
func (u *ACLUser) SetManagementPolicies(p xpv1.ManagementPolicies) {
	u.Spec.ManagementPolicies = p
}

// GetProviderConfigReference of this ACLUser.
func (u *ACLUser) GetProviderConfigReference() *xpv1.ProviderConfigReference {
	return u.Spec.ProviderConfigReference
}

// SetProviderConfigReference of this ACLUser.
func (u *ACLUser) SetProviderConfigReference(r *xpv1.ProviderConfigReference) {
	u.Spec.ProviderConfigReference = r
}

// GetWriteConnectionSecretToReference of this ACLUser.
func (u *ACLUser) GetWriteConnectionSecretToReference() *xpv1.LocalSecretReference {
	return u.Spec.WriteConnectionSecretToReference
}

// SetWriteConnectionSecretToReference of this ACLUser.
func (u *ACLUser) SetWriteConnectionSecretToReference(r *xpv1.LocalSecretReference) {
	u.Spec.WriteConnectionSecretToReference = r
}

// +kubebuilder:object:root=true

// ACLUserList contains a list of ACLUser.
type ACLUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ACLUser `json:"items"`
}

// ACLUser type metadata.
var (
	ACLUserKind             = reflect.TypeOf(ACLUser{}).Name()
	ACLUserGroupKind        = schema.GroupKind{Group: Group, Kind: ACLUserKind}.String()
	ACLUserKindAPIVersion   = ACLUserKind + "." + SchemeGroupVersion.String()
	ACLUserGroupVersionKind = SchemeGroupVersion.WithKind(ACLUserKind)
)
