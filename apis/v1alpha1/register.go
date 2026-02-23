package v1alpha1

// ProviderConfig type metadata.
var (
	ProviderConfigGroupVersionKind = SchemeGroupVersion.WithKind("ProviderConfig")

	ProviderConfigUsageGroupVersionKind     = SchemeGroupVersion.WithKind("ProviderConfigUsage")
	ProviderConfigUsageListGroupVersionKind = SchemeGroupVersion.WithKind("ProviderConfigUsageList")
)

func init() {
	SchemeBuilder.Register(
		&ProviderConfig{},
		&ProviderConfigList{},
		&ProviderConfigUsage{},
		&ProviderConfigUsageList{},
		&ACLUser{},
		&ACLUserList{},
	)
}
