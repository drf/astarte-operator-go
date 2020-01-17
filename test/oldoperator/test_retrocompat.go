package oldoperator

import (
	goctx "context"
	"fmt"
	"testing"

	framework "github.com/operator-framework/operator-sdk/pkg/test"

	operator "github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	"github.com/astarte-platform/astarte-kubernetes-operator/test/utils"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func astarteEnsureRetrocompatTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}

	// Wait until the Deployment turns green.
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		astarteObj := &operator.Astarte{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Namespace: namespace, Name: utils.AstarteTestResource.GetName()}, astarteObj)
		if err != nil {
			return false, err
		}
		if astarteObj.Status.Health != "green" {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}

	// Check all the StatefulSets
	if err := utils.EnsureStatefulSetReadiness(namespace, "example-astarte-cfssl", f); err != nil {
		return err
	}
	if err := utils.EnsureStatefulSetReadiness(namespace, "example-astarte-cassandra", f); err != nil {
		return err
	}
	if err := utils.EnsureStatefulSetReadiness(namespace, "example-astarte-rabbitmq", f); err != nil {
		return err
	}

	// Check if API deployments + DUP are ready. If they are, we're done.
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-appengine-api", f); err != nil {
		return err
	}
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-housekeeping-api", f); err != nil {
		return err
	}
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-pairing-api", f); err != nil {
		return err
	}
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-realm-management-api", f); err != nil {
		return err
	}
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-trigger-engine", f); err != nil {
		return err
	}
	if err := utils.EnsureDeploymentReadiness(namespace, "example-astarte-data-updater-plant", f); err != nil {
		return err
	}

	// Check VerneMQ last thing
	if err := utils.EnsureStatefulSetReadiness(namespace, "example-astarte-vernemq", f); err != nil {
		return err
	}

	return nil
}
