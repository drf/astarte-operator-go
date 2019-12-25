package astartevoyageringress

import (
	"context"

	voyager "github.com/astarte-platform/astarte-kubernetes-operator/external/voyager/v1beta1"
	apiv1alpha1 "github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/misc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func isIngressReady(ingressName string, cr *apiv1alpha1.AstarteVoyagerIngress, c client.Client) bool {
	ingress := &voyager.Ingress{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: ingressName, Namespace: cr.Namespace}, ingress); err != nil {
		// Don't stress it too much.
		return false
	}

	// What type of ingress is it?
	if val, ok := ingress.Annotations[voyager.LBType]; ok {
		if val != voyager.LBTypeLoadBalancer {
			// If the ingress is not a Load Balancer, it's ready as soon as it's up.
			return true
		}
	}
	// Default type is LoadBalancer

	if len(ingress.Status.LoadBalancer.Ingress) == 0 {
		return false
	}

	for _, k := range ingress.Status.LoadBalancer.Ingress {
		if k.IP != "" || k.Hostname != "" {
			return true
		}
	}

	return false
}

func reconcileConfigMap(objName string, data map[string]string, cr *apiv1alpha1.AstarteVoyagerIngress, c client.Client, scheme *runtime.Scheme) (controllerutil.OperationResult, error) {
	return misc.ReconcileConfigMap(objName, data, cr, c, scheme, log)
}

func reconcileSecret(objName string, data map[string][]byte, cr *apiv1alpha1.AstarteVoyagerIngress, c client.Client, scheme *runtime.Scheme) (controllerutil.OperationResult, error) {
	return misc.ReconcileSecret(objName, data, cr, c, scheme, log)
}

func reconcileSecretString(objName string, data map[string]string, cr *apiv1alpha1.AstarteVoyagerIngress, c client.Client, scheme *runtime.Scheme) (controllerutil.OperationResult, error) {
	return misc.ReconcileSecretString(objName, data, cr, c, scheme, log)
}

func logCreateOrUpdateOperationResult(result controllerutil.OperationResult, cr *apiv1alpha1.AstarteVoyagerIngress, obj metav1.Object) {
	misc.LogCreateOrUpdateOperationResult(log, result, cr, obj)
}
