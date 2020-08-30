/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tenantcluster

import (
	"context"

	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

//go:generate mockgen -source=./client.go -destination=./mock/client_generated.go -package=mock
const (
	ConfigMapNamespace = "openshift-config"
	ConfigMapName      = "cloud-provider-config"
	ConfigMapKeyName   = "namespace"
)

// Client is a wrapper object for actual tenant-cluster clients: kubernetesClient and runtimeClient
type Client interface {
	PatchMachine(machine *machinev1.Machine, originMachineCopy *machinev1.Machine) error
	StatusPatchMachine(machine *machinev1.Machine, originMachineCopy *machinev1.Machine) error
	GetSecret(secretName string, namespace string) (*corev1.Secret, error)
	GetNamespace() (string, error)
}

type kubeClient struct {
	kubernetesClient *kubernetes.Clientset
	runtimeClient    client.Client
}

// New creates our client wrapper object for the actual KubeVirt and VirtCtl clients we use.
func New(mgr manager.Manager) (Client, error) {
	kubernetesClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	return &kubeClient{
		kubernetesClient: kubernetesClient,
		runtimeClient:    mgr.GetClient(),
	}, nil
}

func (c *kubeClient) PatchMachine(machine *machinev1.Machine, originMachineCopy *machinev1.Machine) error {
	return c.runtimeClient.Patch(context.Background(), machine, client.MergeFrom(originMachineCopy))
}

func (c *kubeClient) StatusPatchMachine(machine *machinev1.Machine, originMachineCopy *machinev1.Machine) error {
	return c.runtimeClient.Status().Patch(context.Background(), machine, client.MergeFrom(originMachineCopy))
}

func (c *kubeClient) GetSecret(secretName string, namespace string) (*corev1.Secret, error) {
	return c.kubernetesClient.CoreV1().Secrets(namespace).Get(secretName, k8smetav1.GetOptions{})
}

func (c *kubeClient) GetNamespace() (string, error) {
	configMap, err := c.kubernetesClient.CoreV1().ConfigMaps(ConfigMapNamespace).Get(ConfigMapName, k8smetav1.GetOptions{})
	if err != nil {
		return "", err
	}
	vmNamespace, ok := configMap.Data[ConfigMapKeyName]
	if !ok {
		return "", machinecontroller.InvalidMachineConfiguration("Tenant-cluster configMap %s/%s: %v doesn't contain the key %s", ConfigMapNamespace, ConfigMapName, ConfigMapKeyName)
	}
	return vmNamespace, nil
}
