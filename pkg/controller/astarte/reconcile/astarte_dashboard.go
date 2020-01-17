package reconcile

import (
	"context"
	"encoding/json"

	apiv1alpha1 "github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/misc"
	"github.com/openlyinc/pointy"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureAstarteDashboard reconciles Astarte Dashboard
func EnsureAstarteDashboard(cr *apiv1alpha1.Astarte, dashboard apiv1alpha1.AstarteDashboardSpec, c client.Client, scheme *runtime.Scheme) error {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name, "Astarte.Component", "dashboard")
	deploymentName := cr.Name + "-dashboard"
	serviceName := cr.Name + "-dashboard"
	labels := map[string]string{
		"app":               deploymentName,
		"component":         "astarte",
		"astarte-component": "dashboard",
	}
	matchLabels := map[string]string{"app": deploymentName}

	// Ok. Shall we deploy?
	if !pointy.BoolValue(dashboard.GenericClusteredResource.Deploy, true) {
		reqLogger.V(1).Info("Skipping Astarte Dashboard Deployment")
		// Before returning - check if we shall clean up the Deployment.
		// It is the only thing actually requiring resources, the rest will be cleaned up eventually when the
		// Astarte resource is deleted.
		theDeployment := &appsv1.Deployment{}
		err := c.Get(context.TODO(), types.NamespacedName{Name: deploymentName, Namespace: cr.Namespace}, theDeployment)
		if err == nil {
			reqLogger.Info("Deleting previously existing Component Deployment, which is no longer needed")
			if err = c.Delete(context.TODO(), theDeployment); err != nil {
				return err
			}
		}

		// That would be all for today.
		return nil
	}

	// Good. Reconcile the ConfigMap.
	if _, err := reconcileConfigMap(deploymentName+"-config", getAstarteDashboardConfigMapData(cr, dashboard), cr, c, scheme); err != nil {
		return err
	}

	// Good. Now, reconcile the service first of all.
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: cr.Namespace}}
	if result, err := controllerutil.CreateOrUpdate(context.TODO(), c, service, func() error {
		if err := controllerutil.SetControllerReference(cr, service, scheme); err != nil {
			return err
		}
		// Always set everything to what we require.
		service.ObjectMeta.Labels = labels
		service.Spec.Type = v1.ServiceTypeClusterIP
		service.Spec.ClusterIP = "None"
		service.Spec.Ports = []v1.ServicePort{
			v1.ServicePort{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromString("http"),
				Protocol:   v1.ProtocolTCP,
			},
		}
		service.Spec.Selector = matchLabels
		return nil
	}); err == nil {
		logCreateOrUpdateOperationResult(result, cr, service)
	} else {
		return err
	}

	deploymentSpec := appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: matchLabels,
		},
		Strategy: cr.Spec.DeploymentStrategy,
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: getAstarteDashboardPodSpec(deploymentName, cr, dashboard),
		},
	}

	// Build the Deployment
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: cr.Namespace}}
	result, err := controllerutil.CreateOrUpdate(context.TODO(), c, deployment, func() error {
		if err := controllerutil.SetControllerReference(cr, deployment, scheme); err != nil {
			return err
		}

		// Assign the Spec.
		deployment.ObjectMeta.Labels = labels
		deployment.Spec = deploymentSpec
		deployment.Spec.Replicas = dashboard.GenericClusteredResource.Replicas

		return nil
	})
	if err != nil {
		return err
	}

	logCreateOrUpdateOperationResult(result, cr, deployment)
	return nil
}

func getAstarteDashboardPodSpec(deploymentName string, cr *apiv1alpha1.Astarte, dashboard apiv1alpha1.AstarteDashboardSpec) v1.PodSpec {
	component := apiv1alpha1.Dashboard
	ps := v1.PodSpec{
		TerminationGracePeriodSeconds: pointy.Int64(30),
		ImagePullSecrets:              cr.Spec.ImagePullSecrets,
		Containers: []v1.Container{
			v1.Container{
				Name: "dashboard",
				Ports: []v1.ContainerPort{
					v1.ContainerPort{Name: "http", ContainerPort: 80},
				},
				VolumeMounts:    getAstarteDashboardVolumeMounts(),
				Image:           getAstarteImageForClusteredResource(component.DockerImageName(), dashboard.GenericClusteredResource, cr),
				ImagePullPolicy: getImagePullPolicy(cr),
				Resources:       misc.GetResourcesForAstarteComponent(cr, dashboard.GenericClusteredResource.Resources, component),
				Env:             getAstarteDashboardEnvVars(),
			},
		},
		Volumes: getAstarteDashboardVolumes(cr),
	}

	return ps
}

func getAstarteDashboardConfigMapData(cr *apiv1alpha1.Astarte, dashboard apiv1alpha1.AstarteDashboardSpec) map[string]string {
	dashboardConfig := make(map[string]interface{})
	if dashboard.Config.RealmManagementAPIURL != "" {
		dashboardConfig["realm_management_api_url"] = getBaseAstarteAPIURL(cr) + "/realmmanagement/v1/"
	} else {
		dashboardConfig["realm_management_api_url"] = dashboard.Config.RealmManagementAPIURL
	}
	if dashboard.Config.DefaultRealm != "" {
		dashboardConfig["default_realm"] = dashboard.Config.DefaultRealm
	}
	if dashboard.Config.DefaultAuth != "" {
		dashboardConfig["default_auth"] = dashboard.Config.DefaultAuth
	} else {
		dashboardConfig["default_auth"] = "token"
	}
	if len(dashboard.Config.Auth) > 0 {
		dashboardConfig["auth"] = dashboard.Config.Auth
	} else {
		dashboardConfig["auth"] = []apiv1alpha1.AstarteDashboardConfigAuthSpec{apiv1alpha1.AstarteDashboardConfigAuthSpec{Type: "token"}}
	}

	configJSON, _ := json.Marshal(dashboardConfig)

	return map[string]string{
		"config.json": string(configJSON),
	}
}

func getAstarteDashboardVolumes(cr *apiv1alpha1.Astarte) []v1.Volume {
	return []v1.Volume{
		v1.Volume{
			Name: "config",
			VolumeSource: v1.VolumeSource{ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{Name: cr.Name + "-dashboard-config"},
				Items: []v1.KeyToPath{
					v1.KeyToPath{
						Key:  "config.json",
						Path: "config.json",
					},
				},
			}},
		},
	}
}

func getAstarteDashboardVolumeMounts() []v1.VolumeMount {
	ret := []v1.VolumeMount{
		v1.VolumeMount{
			Name:      "config",
			MountPath: "/usr/share/nginx/html/user-config",
			ReadOnly:  true,
		},
	}

	return ret
}

func getAstarteDashboardEnvVars() []v1.EnvVar {
	ret := []v1.EnvVar{
		v1.EnvVar{
			Name:      "MY_POD_IP",
			ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "status.podIP"}},
		},
	}

	return ret
}
