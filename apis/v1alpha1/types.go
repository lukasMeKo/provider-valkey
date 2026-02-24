package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
)

// ProviderConfigSpec defines the desired state of a ProviderConfig.
type ProviderConfigSpec struct {
	// Credentials required to authenticate to Valkey.
	Credentials ProviderCredentials `json:"credentials"`
}

// ProviderCredentials holds the credentials to connect to Valkey.
type ProviderCredentials struct {
	// Source of the credentials.
	// +kubebuilder:validation:Enum=Secret
	Source xpv1.CredentialsSource `json:"source"`

	// A SecretRef is a reference to a secret key that contains the credentials
	// to connect to Valkey. The secret must contain keys: endpoint, port,
	// and optionally username and password.
	// +optional
	xpv1.CommonCredentialSelectors `json:",inline"`

	// TLS configures TLS for the Valkey connection.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// TLSConfig holds TLS settings for the Valkey connection.
type TLSConfig struct {
	// Enabled controls whether TLS is used for the connection.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
}

// +kubebuilder:object:root=true

// ProviderConfig is the configuration for the Valkey provider.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec        `json:"spec"`
	Status xpv1.ProviderConfigStatus `json:"status,omitempty"`
}

// GetCondition of this ProviderConfig.
func (p *ProviderConfig) GetCondition(ct xpv1.ConditionType) xpv1.Condition {
	return p.Status.GetCondition(ct)
}

// SetConditions of this ProviderConfig.
func (p *ProviderConfig) SetConditions(c ...xpv1.Condition) {
	p.Status.SetConditions(c...)
}

// GetUsers of this ProviderConfig.
func (p *ProviderConfig) GetUsers() int64 {
	return p.Status.Users
}

// SetUsers of this ProviderConfig.
func (p *ProviderConfig) SetUsers(i int64) {
	p.Status.Users = i
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig.
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

// +kubebuilder:object:root=true

// ProviderConfigUsage tracks usage of a ProviderConfig.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="CONFIG-NAME",type="string",JSONPath=".providerConfigRef.name"
// +kubebuilder:printcolumn:name="RESOURCE-KIND",type="string",JSONPath=".resourceRef.kind"
// +kubebuilder:printcolumn:name="RESOURCE-NAME",type="string",JSONPath=".resourceRef.name"
// +kubebuilder:resource:scope=Cluster
type ProviderConfigUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// ProviderConfigReference to the provider config being used.
	ProviderConfigRef xpv1.ProviderConfigReference `json:"providerConfigRef"`

	// ResourceReference to the managed resource using the provider config.
	ResourceRef xpv1.TypedReference `json:"resourceRef"`
}

// GetProviderConfigReference of this ProviderConfigUsage.
func (p *ProviderConfigUsage) GetProviderConfigReference() xpv1.ProviderConfigReference {
	return p.ProviderConfigRef
}

// SetProviderConfigReference of this ProviderConfigUsage.
func (p *ProviderConfigUsage) SetProviderConfigReference(r xpv1.ProviderConfigReference) {
	p.ProviderConfigRef = r
}

// GetResourceReference of this ProviderConfigUsage.
func (p *ProviderConfigUsage) GetResourceReference() xpv1.TypedReference {
	return p.ResourceRef
}

// SetResourceReference of this ProviderConfigUsage.
func (p *ProviderConfigUsage) SetResourceReference(r xpv1.TypedReference) {
	p.ResourceRef = r
}

// +kubebuilder:object:root=true

// ProviderConfigUsageList contains a list of ProviderConfigUsage.
type ProviderConfigUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfigUsage `json:"items"`
}

// GetItems returns the list of ProviderConfigUsages as resource.ProviderConfigUsage interfaces.
func (l *ProviderConfigUsageList) GetItems() []resource.ProviderConfigUsage {
	items := make([]resource.ProviderConfigUsage, len(l.Items))
	for i := range l.Items {
		items[i] = &l.Items[i]
	}
	return items
}
