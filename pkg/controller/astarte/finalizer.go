package astarte

import (
	"context"
	"strings"

	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const astarteFinalizer = "finalizer.astarte.astarte-platform.org"

func (r *ReconcileAstarte) finalizeAstarte(cr *v1alpha1.Astarte) error {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name)
	reqLogger.Info("Finalizing Astarte")

	// First of all - do we have the CA Secret still around?
	theSecret := &v1.Secret{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name + "-cfssl-ca", Namespace: cr.Namespace}, theSecret); err == nil {
		// The secret is there. Delete it.
		if err := r.client.Delete(context.TODO(), theSecret); err != nil {
			reqLogger.Error(err, "Error while finalizing Astarte. CFSSL CA Secret will need to be manually removed.")
		}
	}

	// Now it's time for our persistent volume claims. Look up all volumes, and see if we need to clear them out.
	// Information in these volumes becomes meaningless after the instance deletion. If one wants to preserve Cassandra, he should take
	// different measures.
	erasePVCPrefixes := []string{
		cr.Name + "-vernemq-data",
		cr.Name + "-rabbitmq-data",
		cr.Name + "-cfssl-data",
		cr.Name + "-cassandra-data",
	}

	pvcs := &v1.PersistentVolumeClaimList{}
	if err := r.client.List(context.TODO(), pvcs, client.InNamespace(cr.Namespace)); err == nil {
		// Iterate and delete
		for _, pvc := range pvcs.Items {
			for _, prefix := range erasePVCPrefixes {
				if strings.HasPrefix(pvc.GetName(), prefix) {
					// Delete.
					if err := r.client.Delete(context.TODO(), &pvc); err != nil {
						reqLogger.Error(err, "Error while finalizing Astarte. A PersistentVolumeClaim will need to be manually removed.", "PVC", pvc)
					}
					break
				}
			}
		}
	} else if !errors.IsNotFound(err) {
		// Notify
		return err
	}

	// That's it. So long, and thanks for all the fish.
	reqLogger.Info("Successfully finalized astarte")
	return nil
}

func (r *ReconcileAstarte) addFinalizer(cr *v1alpha1.Astarte) error {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name)
	reqLogger.Info("Adding Astarte Finalizer")
	cr.SetFinalizers(append(cr.GetFinalizers(), astarteFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), cr)
	if err != nil {
		reqLogger.Error(err, "Failed to update Astarte with finalizer")
		return err
	}
	return nil
}
