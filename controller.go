package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
	// Corrected import path for the Karpenter v1beta1 API
	karpenterv1beta1 "sigs.k8s.io/karpenter/pkg/apis/v1beta1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const ScalewayFinalizer = "scaleway.com/finalizer"

// ScalewayReconciler reconciles a NodeClaim object.
type ScalewayReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	scwClient *scw.Client
}

//+kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var nodeClaim karpenterv1beta1.NodeClaim
	if err := r.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check for the specific capacity type
	isScalewayGPU := false
	for _, requirement := range nodeClaim.Spec.Requirements {
		if requirement.Key == karpenterv1beta1.CapacityTypeLabel && requirement.Operator == v1.NodeSelectorOpIn {
			for _, value := range requirement.Values {
				if value == "scaleway-gpu" {
					isScalewayGPU = true
					break
				}
			}
		}
		if isScalewayGPU {
			break
		}
	}

	if !isScalewayGPU {
		logger.Info("ignoring nodeclaim, not a scaleway-gpu capacity type", "nodeclaim", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Handle finalizer
	if nodeClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&nodeClaim, ScalewayFinalizer) {
			controllerutil.AddFinalizer(&nodeClaim, ScalewayFinalizer)
			if err := r.Update(ctx, &nodeClaim); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(&nodeClaim, ScalewayFinalizer) {
			logger.Info("TODO: Delete the Scaleway instance")
			// Add deletion logic here

			controllerutil.RemoveFinalizer(&nodeClaim, ScalewayFinalizer)
			if err := r.Update(ctx, &nodeClaim); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	server, err := r.provisionInstance(ctx, &nodeClaim)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Scaleway instance created", "serverID", server.ID)

	return ctrl.Result{}, nil
}

func (r *ScalewayReconciler) provisionInstance(ctx context.Context, nodeClaim *karpenterv1beta1.NodeClaim) (*instance.Server, error) {
	logger := log.FromContext(ctx)

	// Get instance type from NodeClaim requirements
	var instanceType string
	for _, requirement := range nodeClaim.Spec.Requirements {
		if requirement.Key == v1.LabelInstanceTypeStable && requirement.Operator == v1.NodeSelectorOpIn {
			if len(requirement.Values) > 0 {
				instanceType = requirement.Values[0] // Taking the first value
				break
			}
		}
	}

	if instanceType == "" {
		return nil, fmt.Errorf("nodeclaim %q does not have required label %q", nodeClaim.Name, v1.LabelInstanceTypeStable)
	}

	// Translate to Scaleway commercial type
	commercialType, err := getCommercialType(instanceType)
	if err != nil {
		logger.Error(err, "unsupported instance type", "instance-type", instanceType)
		return nil, err
	}

	// For now, using placeholder values for cluster name and token.
	const clusterName = "my-kubernetes-cluster"
	const bootstrapToken = "abcdef.1234567890abcdef"

	userData := generateUserData(clusterName, bootstrapToken)

	instanceAPI := instance.NewAPI(r.scwClient)

	createServerReq := &instance.CreateServerRequest{
		Zone:           "fr-par-1",
		CommercialType: commercialType,
		Image:          "ubuntu_jammy_gpu_os_12",
		Name:           nodeClaim.Name,
		CloudInit:      &userData,
	}

	logger.Info("creating scaleway instance with cloud-init", "commercialType", commercialType)
	resp, err := instanceAPI.CreateServer(createServerReq, scw.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	return resp.Server, nil
}

// getCommercialType translates a Karpenter instance type to a Scaleway commercial type.
func getCommercialType(instanceType string) (string, error) {
	instanceTypeMap := map[string]string{
		"l4":   "L4-1-24G",
		"l40s": "L40S-1-48G",
	}

	commercialType, ok := instanceTypeMap[strings.ToLower(instanceType)]
	if !ok {
		return "", fmt.Errorf("unsupported instance type: %s", instanceType)
	}
	return commercialType, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
    // Add karpenter v1beta1 to the scheme
	if err := karpenterv1beta1.SchemeBuilder.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&karpenterv1beta1.NodeClaim{}).
		Complete(r)
}
