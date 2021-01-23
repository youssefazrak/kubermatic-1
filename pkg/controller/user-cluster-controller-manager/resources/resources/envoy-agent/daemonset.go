/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

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

package envoyagent

import (
	"fmt"

	"k8c.io/kubermatic/v2/pkg/resources"
	"k8c.io/kubermatic/v2/pkg/resources/reconciling"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	utilpointer "k8s.io/utils/pointer"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	defaultResourceRequirements = map[string]*corev1.ResourceRequirements{
		resources.EnvoyAgentDaemonSetName: {
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
				corev1.ResourceCPU:    resource.MustParse("50m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
				corev1.ResourceCPU:    resource.MustParse("100m"),
			},
		},
	}
)

const (
	envoyImageName = "envoyproxy/envoy"
	envoyImageTag  = "v1.17.0"
)

// DaemonSetCreator returns the function to create and update the Envoy DaemonSet
func DaemonSetCreator() reconciling.NamedDaemonSetCreatorGetter {
	return func() (string, reconciling.DaemonSetCreator) {
		return resources.EnvoyAgentDaemonSetName, func(ds *appsv1.DaemonSet) (*appsv1.DaemonSet, error) {
			ds.Name = resources.EnvoyAgentDaemonSetName
			ds.Namespace = metav1.NamespaceSystem
			ds.Labels = resources.BaseAppLabels(resources.EnvoyAgentDaemonSetName, nil)

			ds.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: resources.BaseAppLabels(resources.EnvoyAgentDaemonSetName,
					map[string]string{"app.kubernetes.io/name": "envoy-agent"}),
			}

			// has to be the same as the selector
			ds.Spec.Template.ObjectMeta = metav1.ObjectMeta{
				Labels: resources.BaseAppLabels(resources.EnvoyAgentDaemonSetName,
					map[string]string{"app.kubernetes.io/name": "envoy-agent"}),
			}

			//TODO(youssefazrak) needed?
			ds.Spec.Template.Spec.PriorityClassName = "system-cluster-critical"
			ds.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst

			ds.Spec.Template.Spec.HostNetwork = true

			volumes := getVolumes()
			ds.Spec.Template.Spec.Volumes = volumes

			ds.Spec.Template.Spec.Containers = getContainers()
			err := resources.SetResourceRequirements(ds.Spec.Template.Spec.Containers, defaultResourceRequirements, nil, ds.Annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to set resource requirements: %v", err)
			}

			ds.Spec.Template.Spec.ServiceAccountName = resources.CoreDNSServiceAccountName

			return ds, nil
		}
	}
}

func getContainers() []corev1.Container {
	return []corev1.Container{
		{
			Name:            resources.EnvoyAgentDaemonSetName,
			Image:           fmt.Sprintf("%s/%s:%s", resources.RegistryDocker, envoyImageName, envoyImageTag),
			ImagePullPolicy: corev1.PullIfNotPresent,

			// This amount of logs will be kept for the Tech Preview of
			// the new expose strategy
			Args: []string{"--config-path", "etc/envoy/envoy.yaml", "--component-log-level", "upstream:trace,connection:trace,http:trace,router:trace,filter:trace"},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-volume",
					MountPath: "/etc/envoy/envoy.yaml",
					SubPath:   "envoy.yaml",
				},
			},
			//TODO(youssefazrak) Verify this is not blocking traffic
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{
						"NET_BIND_SERVICE",
					},
					Drop: []corev1.Capability{
						"all",
					},
				},
			},
		},
	}
}

func getVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: utilpointer.Int32Ptr(corev1.ConfigMapVolumeSourceDefaultMode),
					LocalObjectReference: corev1.LocalObjectReference{
						Name: resources.EnvoyAgentConfigMapName,
					},
				},
			},
		},
	}
}
