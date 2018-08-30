/*

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPSet is a set of ip
type IPSet []string

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VerticalPodAutoscaler is the configuration for a vertical pod autoscaler.
type VerticalPodAutoscaler struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the behavior of the autoscaler.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status.
	Spec VerticalPodAutoscalerSpec `json:"spec" protobuf:"bytes,2,name=spec"`

	// Current information about the autoscaler.
	// +optional
	Status VerticalPodAutoscalerStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// VerticalPodAutoscalerSpec is the specification of the behavior of the autoscaler.
type VerticalPodAutoscalerSpec struct {
	// The deployment need to gray release or ref pod need to delete
	DeploymentName string `json:"deploymentName,omitempty" protobuf:"bytes,1,opt,name=deploymentName"`

	// Specify the execute time of grayscale upgrade, if it's not set, means that executed immediately
	// +optional
	ScaleTimestamp string `json:"scaleTimestamp,omitempty" protobuf:"bytes,2,opt,name=scaleTimestamp"`

	// Specify these IPs for pod need to delete or upgrade
	IPSet IPSet `json:"ipSet,omitempty" protobuf:"bytes,3,opt,name=ipSet"`

	// Specify the resources of container which need to upgrade
	Resources *UpdatedContainerResources `json:"resources,omitempty" protobuf:"bytes,4,opt,name=resources"`

	// The VerticalPodAutoscaler strategy to express pod need to delete or upgrade
	// +optional
	Strategy VerticalPodAutoscalerStrategy `json:"strategy,omitempty" protobuf:"bytes,5,opt,name=strategy"`
}

// VerticalPodAutoscalerStrategy describes pod delete or upgrade.
type VerticalPodAutoscalerStrategy struct {
	// Type of VerticalPodAutoscaler. Can be "PodUpgrade" or "PodDelete". Default is PodUpgrade.
	// +optional
	Type VerticalPodAutoscalerStrategyType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// PodActionPhase if type is PodDelete, then it should assign value
	Phase PodActionPhase `json:"phase,omitempty" protobuf:"bytes,2,opt,name=phase"`
}

type VerticalPodAutoscalerStrategyType string

const (
	// PodUpgradeStrategyType specify deployment grayscale upgrade.
	PodUpgradeStrategyType VerticalPodAutoscalerStrategyType = "PodUpgrade"

	// PodDeleteStrategyType means the Pod IP in IPSet need to delete.
	PodDeleteStrategyType VerticalPodAutoscalerStrategyType = "PodDelete"
)

type PodActionPhase string

// controller watch the type, if the type is "Bind", controller will tag the pod with
// annotation(app.suning.com/scaledown: true), but pod don't be delete, only if the type change to be "Delete",
// controller will adjust the deployment replicas and replicaset controller will find these pods and delete.
const (
	// Bind is default, means the pod need be deleted later.
	Binding PodActionPhase = "Binding"

	// Delete means the pod will be deleted now.
	Deleting PodActionPhase = "Deleting"
)

// UpdatedContainerResources is a set of ContainerResource.
type UpdatedContainerResources struct {
	Containers []ContainerResource `json:"containers,omitempty" protobuf:"bytes,1,opt,name=containers"`
}

// ContainerResource specify the resource and image of container which need to update.
type ContainerResource struct {
	// Name of the container.
	ContainerName string `json:"containerName,omitempty" protobuf:"bytes,1,opt,name=containerName"`
	// Docker image name.
	// More info: https://kubernetes.io/docs/concepts/containers/images
	// This field is optional to allow higher level config management to default or override
	// container images in workload controllers like Deployments and StatefulSets.
	// +optionals
	Image string `json:"image,omitempty" protobuf:"bytes,2,opt,name=image"`
	// Compute Resources required by this container.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#resources
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,3,opt,name=resources"`
}

// VerticalPodAutoscalerStatus specify the status of VerticalPodAutoscaler.
type VerticalPodAutoscalerStatus struct {
	// DeploymentUpdated describes the binding deployment whether updated.
	DeploymentUpdated bool `json:"updated,omitempty" protobuf:"bytes,1,opt,name=updated"`
	// Conditions is the set of conditions required for this autoscaler to scale its target,
	// and indicates whether or not those conditions are met.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []VerticalPodAutoscalerCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`
}

// VerticalPodAutoscalerCondition describes the state of
// a VerticalPodAutoscaler at a certain point.
type VerticalPodAutoscalerCondition struct {
	// The last time this condition was updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty" protobuf:"bytes,1,opt,name=lastUpdateTime"`
	// reason is the reason for the condition's last update.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`
	// message is a human-readable explanation containing details about
	// the transition
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VerticalPodAutoscalerList is a collection of VerticalPodAutoscaler.
type VerticalPodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is the list of VerticalPodAutoscaler.
	Items []VerticalPodAutoscaler `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	SchemeBuilder.Register(&VerticalPodAutoscaler{}, &VerticalPodAutoscalerList{})
}
