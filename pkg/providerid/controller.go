// providerID package implements a controller to reconcile the providerID spec
// property on nodes in order to identify a machine by a node and vice versa.
// This functionality is traditionally (but not mandatory) a part of a
// cloud-provider implementation and it is what makes auto-scaling works.
package providerid

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/cluster-api-provider-kubevirt/pkg/clients/infracluster"
	"github.com/openshift/cluster-api-provider-kubevirt/pkg/clients/tenantcluster"
)

const IDFormat = "kubevirt://%s/%s"

var _ reconcile.Reconciler = &providerIDReconciler{}

type providerIDReconciler struct {
	client              client.Client
	infraClusterClient  infracluster.Client
	tenantClusterClient tenantcluster.Client
}

// Reconcile make sure a node has a ProviderID set. The providerID is the ID
// of the machine on kubevirt. The ID is the VM.metadata.namespace/VM.metadata.name
// as its guarantee to be unique in a cluster.
func (r *providerIDReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.V(3).Info("Reconciling", "node", request.NamespacedName)

	// Fetch the Node instance
	node := corev1.Node{}
	err := r.client.Get(context.Background(), request.NamespacedName, &node)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("error getting node: %v", err)
	}

	if node.Spec.ProviderID != "" {
		return reconcile.Result{}, nil
	}

	klog.Info("spec.ProviderID is empty, fetching from the infra-cluster", "node", request.NamespacedName)
	id, err := r.getVMName(node.Name)
	if err != nil {
		return reconcile.Result{}, err
	}

	infraClusterNamespace, err := r.tenantClusterClient.GetNamespace()
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get infraCluster namespace: %v", err)
	}
	node.Spec.ProviderID = FormatProviderID(infraClusterNamespace, id)
	if err := r.client.Update(context.Background(), &node); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed updating node %s: %v", node.Name, err)
	}
	return reconcile.Result{}, nil
}

// FormatProviderID consumes the provider ID of the VM and returns
// a standard format to be used by machine and node reconcilers.
// See IDFormat
func FormatProviderID(namespace, name string) string {
	return fmt.Sprintf(IDFormat, namespace, name)
}

func (r *providerIDReconciler) getVMName(nodeName string) (string, error) {
	infraClusterNamespace, err := r.tenantClusterClient.GetNamespace()
	if err != nil {
		return "", fmt.Errorf("failed to get infraCluster namespace: %v", err)
	}

	vmi, err := r.infraClusterClient.GetVirtualMachineInstance(context.Background(), infraClusterNamespace, nodeName, &v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return vmi.Name, nil
}

// Add registers a new provider ID reconciler controller with the controller manager
func Add(mgr manager.Manager, tenantClusterClient tenantcluster.Client, infraClusterClient infracluster.Client) error {
	reconciler, err := newProviderIDReconciler(mgr, tenantClusterClient, infraClusterClient)

	if err != nil {
		return fmt.Errorf("error building reconciler: %v", err)
	}

	c, err := controller.New("provdierID-controller", mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}

	//Watch node changes
	err = c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// newProviderIDReconciler creates a new providerID reconciler
func newProviderIDReconciler(mgr manager.Manager, tenantClusterClient tenantcluster.Client, infraClusterClient infracluster.Client) (*providerIDReconciler, error) {
	r := providerIDReconciler{
		client:              mgr.GetClient(),
		tenantClusterClient: tenantClusterClient,
		infraClusterClient:  infraClusterClient,
	}
	return &r, nil
}
