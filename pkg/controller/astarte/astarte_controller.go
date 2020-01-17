package astarte

import (
	"context"
	"fmt"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	apiv1alpha1 "github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	recon "github.com/astarte-platform/astarte-kubernetes-operator/pkg/controller/astarte/reconcile"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/controller/astarte/upgrade"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/misc"
	"github.com/astarte-platform/astarte-kubernetes-operator/version"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_astarte")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Astarte Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAstarte{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("astarte-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Astarte
	if err := c.Watch(&source.Kind{Type: &apiv1alpha1.Astarte{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	// Watch for changes to secondary resource Deployments and requeue the owner Astarte
	if err := c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &apiv1alpha1.Astarte{},
	}); err != nil {
		return err
	}
	// Watch for changes to secondary resource StatefulSet and requeue the owner Astarte
	if err := c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &apiv1alpha1.Astarte{},
	}); err != nil {
		return err
	}
	// Watch for changes to secondary resource Job and requeue the owner Astarte
	if err := c.Watch(&source.Kind{Type: &batchv1.Job{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &apiv1alpha1.Astarte{},
	}); err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAstarte implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAstarte{}

// ReconcileAstarte reconciles a Astarte object
type ReconcileAstarte struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Astarte object and makes changes based on the state read
// and what is in the Astarte.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAstarte) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Astarte")

	// Fetch the Astarte instance
	instance := &apiv1alpha1.Astarte{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Are we capable of handling the requested version?
	// Build a SemVer out of the requested Astarte Version in the Spec.
	newAstarteSemVersion, err := semver.NewVersion(instance.Spec.Version)
	if err != nil {
		// Reconcile every minute if we're here
		return reconcile.Result{RequeueAfter: time.Minute},
			fmt.Errorf("Could not build a valid Astarte Semantic Version out of requested Astarte Version %v. Refusing to proceed", err)
	}
	// Generate another one for checks, as constraints do not work with pre-releases
	constraintCheckAstarteSemVersion := newAstarteSemVersion
	if constraintCheckAstarteSemVersion.Prerelease() != "" {
		reqLogger.Info("Reconciling an Astarte pre-release: ensure you know what you're doing!")
		noPreVer, _ := constraintCheckAstarteSemVersion.SetPrerelease("")
		constraintCheckAstarteSemVersion = &noPreVer
	}
	constraint, err := semver.NewConstraint(version.AstarteVersionConstraintString)
	if err != nil {
		// Don't reconcile, this is a development failure.
		return reconcile.Result{Requeue: false}, err
	}
	if !constraint.Check(constraintCheckAstarteSemVersion) {
		return reconcile.Result{Requeue: false},
			fmt.Errorf("Astarte version %s is not supported by this Operator! This Operator supports versions respecting this constraint: %s. Please migrate to an Operator supporting this version",
				instance.Spec.Version, version.AstarteVersionConstraintString)
	}

	// Check if the Astarte instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if instance.GetDeletionTimestamp() != nil {
		if contains(instance.GetFinalizers(), astarteFinalizer) {
			// Run finalization logic for astarteFinalizer. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeAstarte(instance); err != nil {
				return reconcile.Result{}, err
			}

			// Remove astarteFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			instance.SetFinalizers(remove(instance.GetFinalizers(), astarteFinalizer))
			if err := r.client.Update(context.TODO(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer for this CR
	if !contains(instance.GetFinalizers(), astarteFinalizer) {
		if err := r.addFinalizer(instance); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Ensure we know which version we're dealing with
	if instance.Status.AstarteVersion == "" {
		// Ok, in this case there's two potential situations: we're on our first reconcile, or the status is
		// messed up. Let's see if we can find the Housekeeping Deployment.
		hkDeployment := &appsv1.Deployment{}
		err := r.client.Get(context.TODO(),
			types.NamespacedName{Name: instance.Name + "-housekeeping", Namespace: instance.Namespace}, hkDeployment)
		if err == nil {
			// In this case, we might be in a weird state (e.g.: migrating from the old operator). Let's try and fix this.
			reqLogger.Info("Astarte Version not found in Status! Trying to reconcile it from Housekeeping's image tag")
			hkImage := hkDeployment.Spec.Template.Spec.Containers[0].Image
			hkImageTokens := strings.Split(hkImage, ":")
			if len(hkImageTokens) != 2 {
				// Reconcile every minute if we're here
				return reconcile.Result{RequeueAfter: time.Minute},
					fmt.Errorf("Could not parse Astarte version from Housekeeping Image tag %s. Refusing to proceed", hkImage)
			}

			instance.Status.AstarteVersion = hkImageTokens[1]
			// Update the status
			if err := r.client.Status().Update(context.TODO(), instance); err != nil {
				reqLogger.Error(err, "Failed to update Astarte status.")
				return reconcile.Result{}, err
			}
		} else if !errors.IsNotFound(err) {
			// There was some issue in reading the Object - requeue
			return reconcile.Result{}, err
		}

		reqLogger.V(1).Info("Apparently running first reconciliation.")
	}

	// First of all, check the current version, and see if we need to transition to an upgrade.
	if instance.Status.AstarteVersion == "" {
		reqLogger.Info("Could not determine an existing Astarte version for this Resource. Assuming this is a new installation.")
	} else if instance.Status.AstarteVersion == "snapshot" {
		reqLogger.Info("You are running an Astarte snapshot. Any upgrade phase will be skipped, you hopefully know what you're doing")
	} else if instance.Status.AstarteVersion != instance.Spec.Version {
		reqLogger.Info("Requested Version and Status Version are different, checking for upgrades...",
			"Version.Old", instance.Status.AstarteVersion, "Version.New", instance.Spec.Version)

		// TODO: This should probably be put in the Admission Webhook too, going forward
		if instance.Status.Health != "green" {
			reqLogger.Error(fmt.Errorf("Astarte Upgrade requested, but the cluster isn't reporting stable Health. Refusing to upgrade"),
				"Cluster health is unstable, refusing to upgrade. Please revert to the previous version and wait for the cluster to settle.",
				"Health", instance.Status.Health)
			return reconcile.Result{Requeue: false}, fmt.Errorf("Astarte Upgrade requested, but the cluster isn't reporting stable Health. Refusing to upgrade")
		}
		// We need to check for upgrades.
		versionString := instance.Status.AstarteVersion
		// Are we on a release snapshot?
		if strings.Contains(instance.Status.AstarteVersion, "-snapshot") {
			// We're running on a release snapshot. Assume it's .0
			versionString = strings.Replace(versionString, "-snapshot", ".0", -1)
			reqLogger.Info("You are running a Release snapshot. This is generally not a good idea in production. Assuming a Release version", "Version", versionString)
		}

		// Build the semantic version
		oldAstarteSemVersion, err := semver.NewVersion(versionString)
		if err != nil {
			// Reconcile every minute if we're here
			return reconcile.Result{RequeueAfter: time.Minute},
				fmt.Errorf("Could not build a valid Astarte Semantic Version out of existing Astarte Version %v. Refusing to proceed", err)
		}

		// Ok! Let's try and upgrade (if needed)
		if err := upgrade.EnsureAstarteUpgrade(oldAstarteSemVersion, newAstarteSemVersion, instance, r.client, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Start actual reconciliation.
	// Start by ensuring the housekeeping key
	if err = recon.EnsureHousekeepingKey(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	// Then, make sure we have an up to date Erlang Configuration for our Pods
	if err = recon.EnsureGenericErlangConfiguration(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Dependencies Dance!
	// RabbitMQ, first and foremost
	if err = recon.EnsureRabbitMQ(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Cassandra
	if err = recon.EnsureCassandra(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// CFSSL
	if err = recon.EnsureCFSSL(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// CFSSL CA Secret
	if err = recon.EnsureCFSSLCASecret(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// OK! Now it's time to reconcile all of Astarte Services, in a specific order.
	// Housekeeping first - it creates/migrates the Database
	if err = recon.EnsureAstarteGenericBackend(instance, instance.Spec.Components.Housekeeping.Backend, apiv1alpha1.Housekeeping, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	// Then Housekeeping API
	if err = recon.EnsureAstarteGenericAPI(instance, instance.Spec.Components.Housekeeping.API, apiv1alpha1.HousekeepingAPI, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Then, Realm Management and its API
	if err = recon.EnsureAstarteGenericBackend(instance, instance.Spec.Components.RealmManagement.Backend, apiv1alpha1.RealmManagement, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	if err = recon.EnsureAstarteGenericAPI(instance, instance.Spec.Components.RealmManagement.API, apiv1alpha1.RealmManagementAPI, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Then, Pairing and its API
	if err = recon.EnsureAstarteGenericBackend(instance, instance.Spec.Components.Pairing.Backend, apiv1alpha1.Pairing, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	if err = recon.EnsureAstarteGenericAPI(instance, instance.Spec.Components.Pairing.API, apiv1alpha1.PairingAPI, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Trigger Engine right before DUP
	if err = recon.EnsureAstarteGenericBackend(instance, instance.Spec.Components.TriggerEngine, apiv1alpha1.TriggerEngine, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Now it's Data Updater plant turn
	if err = recon.EnsureAstarteGenericBackend(instance, instance.Spec.Components.DataUpdaterPlant.GenericClusteredResource, apiv1alpha1.DataUpdaterPlant, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Now it's AppEngine API turn
	if err = recon.EnsureAstarteGenericAPI(instance, instance.Spec.Components.AppengineAPI.GenericAPISpec, apiv1alpha1.AppEngineAPI, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Last but not least, VerneMQ
	if err = recon.EnsureVerneMQ(instance, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// And Dashboard to close it down.
	if err = recon.EnsureAstarteDashboard(instance, instance.Spec.Components.Dashboard, r.client, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Compute overall Readiness for Astarte deployments
	astarteDeployments := &appsv1.DeploymentList{}
	nonReadyDeployments := 0
	if err := r.client.List(context.TODO(), astarteDeployments, client.InNamespace(instance.Namespace),
		client.MatchingLabels{"component": "astarte"}); err == nil {
		for _, deployment := range astarteDeployments.Items {
			if deployment.Status.ReadyReplicas == 0 {
				nonReadyDeployments++
			}
		}
	} else {
		reqLogger.Info("Could not list Astarte deployments to compute health.")
	}

	if nonReadyDeployments == 0 {
		instance.Status.Health = "green"
	} else if nonReadyDeployments == 1 {
		instance.Status.Health = "yellow"
	} else {
		instance.Status.Health = "red"
	}

	// Update status
	instance.Status.AstarteVersion = instance.Spec.Version
	instance.Status.OperatorVersion = version.Version
	instance.Status.ReconciliationPhase = apiv1alpha1.ReconciliationPhaseReconciled
	instance.Status.BaseAPIURL = "https://" + instance.Spec.API.Host
	instance.Status.BrokerURL = misc.GetVerneMQBrokerURL(instance)

	if err := r.client.Status().Update(context.TODO(), instance); err != nil {
		reqLogger.Error(err, "Failed to update Astarte status.")
		return reconcile.Result{}, err
	}

	reqLogger.Info("Astarte Reconciled successfully")
	return reconcile.Result{}, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}
