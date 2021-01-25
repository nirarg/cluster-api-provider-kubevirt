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
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
)

const IDFormat = "kubevirt://%s/%s"

const (
	ConfigMapNamespace             = "openshift-config"
	ConfigMapName                  = "cloud-provider-config"
	ConfigMapDataKeyName           = "config"
	ConfigMapInfraNamespaceKeyName = "namespace"
	ConfigMapInfraIDKeyName        = "infraID"
)

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

	cMap, err := r.tenantClusterClient.GetConfigMapValue(context.Background(), ConfigMapName, ConfigMapNamespace, ConfigMapDataKeyName)
	if err != nil {
		return reconcile.Result{}, nil
	}
	infraClusterNamespace, ok := (*cMap)[ConfigMapInfraNamespaceKeyName]
	if !ok {
		return reconcile.Result{}, machinecontroller.InvalidMachineConfiguration("ProviderID: configMap %s/%s: The map extracted with key %s doesn't contain key %s",
			ConfigMapNamespace, ConfigMapName, ConfigMapDataKeyName, ConfigMapInfraNamespaceKeyName)
	}

	klog.Info("spec.ProviderID is empty, fetching from the infra-cluster", "node", request.NamespacedName)
	id, err := r.getVMName(node.Name, infraClusterNamespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	node.Spec.ProviderID = FormatProviderID(infraClusterNamespace, id)
	err = r.client.Update(context.Background(), &node)
	if err != nil {
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

func (r *providerIDReconciler) getVMName(nodeName string, infraClusterNamespace string) (string, error) {
	vmi, err := r.infraClusterClient.GetVirtualMachineInstance(context.Background(), infraClusterNamespace, nodeName, &v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return vmi.Name, nil
}

// Add registers a new provider ID reconciler controller with the controller manager
func Add(mgr manager.Manager, infraClusterClient infracluster.Client, tenantClusterClient tenantcluster.Client) error {
	reconciler, err := NewProviderIDReconciler(mgr, infraClusterClient, tenantClusterClient)

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

// NewProviderIDReconciler creates a new providerID reconciler
func NewProviderIDReconciler(mgr manager.Manager, infraClusterClient infracluster.Client, tenantClusterClient tenantcluster.Client) (*providerIDReconciler, error) {
	r := providerIDReconciler{
		client:              mgr.GetClient(),
		infraClusterClient:  infraClusterClient,
		tenantClusterClient: tenantClusterClient,
	}
	return &r, nil
}
