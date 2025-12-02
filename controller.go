package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const ScalewayFinalizer = "scaleway.com/finalizer"

// NodeClaim is a local stub that mimics the Karpenter NodeClaim.
// This is used to avoid a direct dependency on the Karpenter API machinery.
// +kubebuilder:object:root=true
type NodeClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NodeClaimSpec `json:"spec,omitempty"`
}

// NodeClaimSpec defines the desired state of NodeClaim
type NodeClaimSpec struct {
	// This is a simplified spec for the stub.
	ProvisionerName string `json:"provisionerName,omitempty"`
}

// Required methods to implement runtime.Object
func (in *NodeClaim) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *NodeClaim) DeepCopy() *NodeClaim {
	if in == nil {
		return nil
	}
	out := new(NodeClaim)
	in.DeepCopyInto(out)
	return out
}

func (in *NodeClaim) DeepCopyInto(out *NodeClaim) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
}

// ScalewayReconciler reconciles a NodeClaim object.
type ScalewayReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	scwClient *scw.Client
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var nodeClaim NodeClaim
	if err := r.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if nodeClaim.Labels["karpenter.sh/capacity-type"] != "scaleway-gpu" {
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
			logger.Info("TODO: Supprimer l'instance Scaleway")

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

func (r *ScalewayReconciler) provisionInstance(ctx context.Context, nodeClaim *NodeClaim) (*instance.Server, error) {
	logger := log.FromContext(ctx)

	// Get instance type from NodeClaim labels
	instanceTypeLabel := "karpenter.sh/instance-type"
	instanceType, ok := nodeClaim.Labels[instanceTypeLabel]
	if !ok {
		return nil, fmt.Errorf("nodeclaim %q does not have label %q", nodeClaim.Name, instanceTypeLabel)
	}

	// Translate to Scaleway commercial type
	commercialType, err := getCommercialType(instanceType)
	if err != nil {
		// Log and return error if instance type is not supported
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
		CommercialType: commercialType, // Use dynamic commercial type
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
	// This map translates Karpenter instance type names to Scaleway commercial types.
	// This should be expanded based on the types you want to support.
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
	// Because we are not using the real Karpenter API, we need to manually
	// add our stub NodeClaim type to the manager's scheme so that the
	// controller can watch for it.
	// We use the same GroupVersion as the real Karpenter API.
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1alpha5"}
	if err := mgr.GetScheme().AddKnownTypes(
		gv,
		&NodeClaim{},
		&NodeClaimList{},
	); err != nil {
		return err
	}
	metav1.AddToGroupVersion(mgr.GetScheme(), gv)

	return ctrl.NewControllerManagedBy(mgr).
		For(&NodeClaim{}).
		Complete(r)
}

// +kubebuilder:object:root=true
type NodeClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeClaim `json:"items"`
}

func (in *NodeClaimList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *NodeClaimList) DeepCopy() *NodeClaimList {
	if in == nil {
		return nil
	}
	out := new(NodeClaimList)
	in.DeepCopyInto(out)
	return out
}

func (in *NodeClaimList) DeepCopyInto(out *NodeClaimList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NodeClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}
