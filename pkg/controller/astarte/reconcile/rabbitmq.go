package reconcile

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	semver "github.com/Masterminds/semver/v3"
	apiv1alpha1 "github.com/astarte-platform/astarte-kubernetes-operator/pkg/apis/api/v1alpha1"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/controller/astarte/deps"
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/misc"
	"github.com/openlyinc/pointy"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureRabbitMQ reconciles the state of RabbitMQ
func EnsureRabbitMQ(cr *apiv1alpha1.Astarte, c client.Client, scheme *runtime.Scheme) error {
	statefulSetName := cr.Name + "-rabbitmq"
	labels := map[string]string{"app": statefulSetName}

	// Validate where necessary
	if err := validateRabbitMQDefinition(cr.Spec.RabbitMQ); err != nil {
		return err
	}

	// Depending on the situation, we need to take action on the credentials.
	createUserCredentialsSecret := true
	createUserCredentialsSecretFromCredentials := false
	if cr.Spec.RabbitMQ.Connection != nil {
		if cr.Spec.RabbitMQ.Connection.Secret != nil {
			createUserCredentialsSecret = false
		} else if cr.Spec.RabbitMQ.Connection.Username != "" {
			createUserCredentialsSecretFromCredentials = true
		}
	}

	if createUserCredentialsSecret {
		userCredentialsSecret := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: statefulSetName + "-user-credentials", Namespace: cr.Namespace}}
		if result, err := controllerutil.CreateOrUpdate(context.TODO(), c, userCredentialsSecret, func() error {
			if err := controllerutil.SetControllerReference(cr, userCredentialsSecret, scheme); err != nil {
				return err
			}
			if createUserCredentialsSecretFromCredentials {
				// Ensure the Data field matches
				userCredentialsSecret.StringData[misc.RabbitMQDefaultUserCredentialsUsernameKey] = cr.Spec.RabbitMQ.Connection.Username
				userCredentialsSecret.StringData[misc.RabbitMQDefaultUserCredentialsPasswordKey] = cr.Spec.RabbitMQ.Connection.Password
			} else {
				// In this case, see if we need to generate new secrets. Otherwise just skip.
				if _, ok := userCredentialsSecret.Data[misc.RabbitMQDefaultUserCredentialsUsernameKey]; !ok {
					// Create a new, random password out of 16 bytes of entropy
					password := make([]byte, 16)
					rand.Read(password)
					userCredentialsSecret.StringData = map[string]string{
						misc.RabbitMQDefaultUserCredentialsUsernameKey: "astarte-admin",
						misc.RabbitMQDefaultUserCredentialsPasswordKey: base64.URLEncoding.EncodeToString(password),
					}
				}
			}
			return nil
		}); err == nil {
			logCreateOrUpdateOperationResult(result, cr, userCredentialsSecret)
		} else {
			return err
		}
	} else {
		// Maybe delete it, if we created it already?
		theSecret := &v1.Secret{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: statefulSetName + "-user-credentials", Namespace: cr.Namespace}, theSecret); err == nil {
			if err := c.Delete(context.TODO(), theSecret); err != nil {
				return err
			}
		}
	}

	// Ok. Shall we deploy?
	if !pointy.BoolValue(cr.Spec.RabbitMQ.GenericClusteredResource.Deploy, true) {
		log.Info("Skipping RabbitMQ Deployment")
		// Before returning - check if we shall clean up the StatefulSet.
		// It is the only thing actually requiring resources, the rest will be cleaned up eventually when the
		// Astarte resource is deleted.
		theStatefulSet := &appsv1.StatefulSet{}
		err := c.Get(context.TODO(), types.NamespacedName{Name: statefulSetName, Namespace: cr.Namespace}, theStatefulSet)
		if err == nil {
			log.Info("Deleting previously existing RabbitMQ StatefulSet, which is no longer needed")
			if err = c.Delete(context.TODO(), theStatefulSet); err != nil {
				return err
			}
		}

		// That would be all for today.
		return nil
	}

	// First of all, check if we need to regenerate the cookie.
	if err := ensureErlangCookieSecret(statefulSetName+"-cookie", cr, c, scheme); err != nil {
		return err
	}

	// Ensure we reconcile with the RBAC Roles, if needed.
	if pointy.BoolValue(cr.Spec.RBAC, true) {
		if err := reconcileStandardRBACForClusteringForApp(statefulSetName, getRabbitMQPolicyRules(), cr, c, scheme); err != nil {
			return err
		}
	}

	// Good. Now, reconcile the service first of all.
	service := &v1.Service{ObjectMeta: getCommonRabbitMQObjectMeta(statefulSetName, cr)}
	if result, err := controllerutil.CreateOrUpdate(context.TODO(), c, service, func() error {
		if err := controllerutil.SetControllerReference(cr, service, scheme); err != nil {
			return err
		}
		// Always set everything to what we require.
		service.Spec.Type = v1.ServiceTypeClusterIP
		service.Spec.ClusterIP = "None"
		service.Spec.Ports = []v1.ServicePort{
			v1.ServicePort{
				Name:       "amqp",
				Port:       5672,
				TargetPort: intstr.FromString("amqp"),
				Protocol:   v1.ProtocolTCP,
			},
			v1.ServicePort{
				Name:       "management",
				Port:       15672,
				TargetPort: intstr.FromString("management"),
				Protocol:   v1.ProtocolTCP,
			},
		}
		service.Spec.Selector = labels
		return nil
	}); err == nil {
		logCreateOrUpdateOperationResult(result, cr, service)
	} else {
		return err
	}

	// Good. Reconcile the ConfigMap.
	if _, err := reconcileConfigMap(statefulSetName+"-config", getRabbitMQConfigMapData(statefulSetName, cr), cr, c, scheme); err != nil {
		return err
	}

	// Let's check upon Storage now.
	dataVolumeName, persistentVolumeClaim := computePersistentVolumeClaim(statefulSetName+"-data", resource.NewScaledQuantity(4, resource.Giga),
		cr.Spec.RabbitMQ.Storage, cr)

	// Compute and prepare all data for building the StatefulSet
	statefulSetSpec := appsv1.StatefulSetSpec{
		ServiceName: statefulSetName,
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: getRabbitMQPodSpec(statefulSetName, dataVolumeName, cr),
		},
	}

	if persistentVolumeClaim != nil {
		statefulSetSpec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{*persistentVolumeClaim}
	}

	// Build the StatefulSet
	rmqStatefulSet := &appsv1.StatefulSet{ObjectMeta: getCommonRabbitMQObjectMeta(statefulSetName, cr)}
	result, err := controllerutil.CreateOrUpdate(context.TODO(), c, rmqStatefulSet, func() error {
		if err := controllerutil.SetControllerReference(cr, rmqStatefulSet, scheme); err != nil {
			return err
		}

		// Assign the Spec.
		rmqStatefulSet.Spec = statefulSetSpec
		rmqStatefulSet.Spec.Replicas = cr.Spec.RabbitMQ.GenericClusteredResource.Replicas

		return nil
	})
	if err != nil {
		return err
	}

	logCreateOrUpdateOperationResult(result, cr, service)
	return nil
}

func validateRabbitMQDefinition(rmq apiv1alpha1.AstarteRabbitMQSpec) error {
	if !pointy.BoolValue(rmq.GenericClusteredResource.Deploy, true) {
		// We need to make sure that we have all needed components
		if rmq.Connection == nil {
			return errors.New("When not deploying RabbitMQ, the 'connection' section is compulsory")
		}
		if rmq.Connection.Host == "" {
			return errors.New("When not deploying RabbitMQ, it is compulsory to specify at least a Host")
		}
		if (rmq.Connection.Username == "" || rmq.Connection.Password == "") && rmq.Connection.Secret == nil {
			return errors.New("When not deploying RabbitMQ, either a username/password combination or a Kubernetes secret must be provided")
		}
	}
	// All is good.
	return nil
}

func getRabbitMQInitContainers() []v1.Container {
	return []v1.Container{
		v1.Container{
			Name:  "copy-rabbitmq-config",
			Image: "busybox",
			Command: []string{
				"sh",
				"-c",
				"cp /configmap/* /etc/rabbitmq",
			},
			VolumeMounts: []v1.VolumeMount{
				v1.VolumeMount{
					Name:      "config-volume",
					MountPath: "/configmap",
				},
				v1.VolumeMount{
					Name:      "config",
					MountPath: "/etc/rabbitmq",
				},
			},
		},
	}
}

func getRabbitMQLivenessProbe() *v1.Probe {
	// rabbitmqctl status is pretty expensive. Don't run it more than once per minute.
	// Also, give it enough time to start.
	return &v1.Probe{
		Handler:             v1.Handler{Exec: &v1.ExecAction{Command: []string{"rabbitmqctl", "status"}}},
		InitialDelaySeconds: 300,
		TimeoutSeconds:      10,
		PeriodSeconds:       60,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
}

func getRabbitMQReadinessProbe() *v1.Probe {
	// Starting at least 3.7.21, rabbitmqctl status fails if the app hasn't started yet, and this could take *a lot*
	// of time. Increase the failure threshold to 15 before giving up.
	return &v1.Probe{
		Handler:             v1.Handler{Exec: &v1.ExecAction{Command: []string{"rabbitmqctl", "status"}}},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      10,
		PeriodSeconds:       30,
		SuccessThreshold:    1,
		FailureThreshold:    15,
	}
}

func getRabbitMQEnvVars(statefulSetName string, cr *apiv1alpha1.Astarte) []v1.EnvVar {
	userCredentialsSecretName, userCredentialsSecretUsernameKey, userCredentialsSecretPasswordKey := misc.GetRabbitMQUserCredentialsSecret(cr)

	return []v1.EnvVar{
		v1.EnvVar{
			Name:  "RABBITMQ_USE_LONGNAME",
			Value: "true",
		},
		v1.EnvVar{
			Name:      "MY_POD_NAME",
			ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"}},
		},
		v1.EnvVar{
			Name:  "RABBITMQ_NODENAME",
			Value: fmt.Sprintf("rabbit@$(MY_POD_NAME).%s.%s.svc.cluster.local", statefulSetName, cr.Namespace),
		},
		v1.EnvVar{
			Name:  "K8S_SERVICE_NAME",
			Value: statefulSetName,
		},
		v1.EnvVar{
			Name: "RABBITMQ_DEFAULT_USER",
			ValueFrom: &v1.EnvVarSource{SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: userCredentialsSecretName},
				Key:                  userCredentialsSecretUsernameKey,
			}},
		},
		v1.EnvVar{
			Name: "RABBITMQ_DEFAULT_PASS",
			ValueFrom: &v1.EnvVarSource{SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: userCredentialsSecretName},
				Key:                  userCredentialsSecretPasswordKey,
			}},
		},
		v1.EnvVar{
			Name: "RABBITMQ_ERLANG_COOKIE",
			ValueFrom: &v1.EnvVarSource{SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: statefulSetName + "-cookie"},
				Key:                  "erlang-cookie",
			}},
		},
	}
}

func getRabbitMQPodSpec(statefulSetName, dataVolumeName string, cr *apiv1alpha1.Astarte) v1.PodSpec {
	serviceAccountName := statefulSetName
	if pointy.BoolValue(cr.Spec.RBAC, false) {
		serviceAccountName = ""
	}
	astarteVersion, _ := semver.NewVersion(cr.Spec.Version)

	ps := v1.PodSpec{
		TerminationGracePeriodSeconds: pointy.Int64(30),
		ServiceAccountName:            serviceAccountName,
		InitContainers:                getRabbitMQInitContainers(),
		ImagePullSecrets:              cr.Spec.ImagePullSecrets,
		Affinity:                      getAffinityForClusteredResource(statefulSetName, cr.Spec.RabbitMQ.GenericClusteredResource),
		Containers: []v1.Container{
			v1.Container{
				Name: "rabbitmq",
				VolumeMounts: []v1.VolumeMount{
					v1.VolumeMount{
						Name:      "config",
						MountPath: "/etc/rabbitmq",
					},
					v1.VolumeMount{
						Name:      dataVolumeName,
						MountPath: "/var/lib/rabbitmq",
					},
				},
				Image: getImageForClusteredResource("rabbitmq", deps.GetDefaultVersionForRabbitMQ(astarteVersion),
					cr.Spec.RabbitMQ.GenericClusteredResource),
				ImagePullPolicy: getImagePullPolicy(cr),
				Ports: []v1.ContainerPort{
					v1.ContainerPort{Name: "amqp", ContainerPort: 5672},
					v1.ContainerPort{Name: "management", ContainerPort: 15672},
				},
				LivenessProbe:  getRabbitMQLivenessProbe(),
				ReadinessProbe: getRabbitMQReadinessProbe(),
				Resources:      cr.Spec.RabbitMQ.GenericClusteredResource.Resources,
				Env:            getRabbitMQEnvVars(statefulSetName, cr),
			},
		},
		Volumes: []v1.Volume{
			v1.Volume{
				Name:         "config",
				VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}},
			},
			v1.Volume{
				Name: "config-volume",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{Name: statefulSetName + "-config"},
						Items: []v1.KeyToPath{
							v1.KeyToPath{
								Key:  "rabbitmq.conf",
								Path: "rabbitmq.conf",
							},
							v1.KeyToPath{
								Key:  "enabled_plugins",
								Path: "enabled_plugins",
							},
						},
					},
				},
			},
		},
	}

	return ps
}

func getRabbitMQConfigMapData(statefulSetName string, cr *apiv1alpha1.Astarte) map[string]string {
	rmqPlugins := []string{"rabbitmq_management", "rabbitmq_peer_discovery_k8s"}
	if len(cr.Spec.RabbitMQ.AdditionalPlugins) > 0 {
		rmqPlugins = append(rmqPlugins, cr.Spec.RabbitMQ.AdditionalPlugins...)
	}

	rmqConf := `## Clustering
cluster_formation.peer_discovery_backend  = rabbit_peer_discovery_k8s
cluster_formation.k8s.host = kubernetes.default.svc.cluster.local
cluster_formation.k8s.hostname_suffix = .%v.%v.svc.cluster.local
cluster_formation.k8s.address_type = hostname
cluster_formation.node_cleanup.interval = 10
cluster_formation.node_cleanup.only_log_warning = true
cluster_partition_handling = autoheal
## queue master locator 
queue_master_locator=min-masters
## enable guest user  
loopback_users.guest = false
`
	rmqConf = fmt.Sprintf(rmqConf, statefulSetName, cr.Namespace)

	return map[string]string{
		"enabled_plugins": fmt.Sprintf("[%s].\n", strings.Join(rmqPlugins, ",")),
		"rabbitmq.conf":   rmqConf,
	}
}

func getRabbitMQPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"endpoints"},
			Verbs:     []string{"get"},
		},
	}
}

func getCommonRabbitMQObjectMeta(statefulSetName string, cr *apiv1alpha1.Astarte) metav1.ObjectMeta {
	labels := map[string]string{"app": statefulSetName}
	return metav1.ObjectMeta{
		Name:      statefulSetName,
		Namespace: cr.Namespace,
		Labels:    labels,
	}
}
