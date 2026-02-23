// Package controller wires all controllers for the provider.
package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"

	"github.com/lukasMeko/provider-valkey/internal/controller/acluser"
	"github.com/lukasMeko/provider-valkey/internal/controller/config"
)

// Setup creates all provider controllers and adds them to the manager.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	if err := config.Setup(mgr, o); err != nil {
		return err
	}
	if err := acluser.Setup(mgr, o); err != nil {
		return err
	}
	return nil
}
