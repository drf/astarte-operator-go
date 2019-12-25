package controller

import (
	"github.com/astarte-platform/astarte-kubernetes-operator/pkg/controller/astarte"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, astarte.Add)
}
