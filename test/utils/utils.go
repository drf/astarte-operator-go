package utils

import (
	goctx "context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	retryInterval        = time.Second * 10
	timeout              = time.Second * 420
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

// EnsureDeploymentReadiness ensures a Deployment is ready by the time the function is called
func EnsureDeploymentReadiness(namespace, name string, f *framework.Framework) error {
	deployment := &appsv1.Deployment{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, deployment)
	if err != nil {
		return err
	}

	if deployment.Status.ReadyReplicas < 1 {
		return fmt.Errorf("Not ready yet")
	}

	return nil
}

// WaitForDeploymentReadiness waits until a Deployment is ready with a reasonable timeout
func WaitForDeploymentReadiness(namespace, name string, f *framework.Framework) error {
	return wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		deployment := &appsv1.Deployment{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, deployment)
		if err != nil {
			return false, err
		}

		if deployment.Status.ReadyReplicas < 1 {
			return false, nil
		}

		return true, nil
	})
}

// EnsureStatefulSetReadiness ensures a StatefulSet is ready by the time the function is called
func EnsureStatefulSetReadiness(namespace, name string, f *framework.Framework) error {
	statefulSet := &appsv1.StatefulSet{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, statefulSet)
	if err != nil {
		return err
	}

	if statefulSet.Status.ReadyReplicas < 1 {
		return fmt.Errorf("Not ready yet")
	}

	return nil
}

// WaitForStatefulSetReadiness waits until a StatefulSet is ready with a reasonable timeout
func WaitForStatefulSetReadiness(namespace, name string, f *framework.Framework) error {
	return wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		statefulSet := &appsv1.StatefulSet{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, statefulSet)
		if err != nil {
			return false, err
		}

		if statefulSet.Status.ReadyReplicas < 1 {
			return false, nil
		}

		return true, nil
	})
}
