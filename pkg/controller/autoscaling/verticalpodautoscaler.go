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

package autoscaling

import (
	"autoscaler/pkg/apis/autoscaling/v1alpha1"
	"autoscaler/pkg/util"
	"context"
	"fmt"
	"sort"

	"time"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// Layout is the time format.
	Layout = "2006-01-02 15:04:05"

	// GrayscaleAnnotation specify the deployment need to grayscale.
	GrayscaleAnnotation = "app.suning.com/grayscale"

	// ScaleDownAnnotation specify the pod need to delete.
	ScaleDownAnnotation = "app.suning.com/scaledown"
)

// Add creates a new VerticalPodAutoscaler Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
// USER ACTION REQUIRED: update cmd/manager/main.go to call this autoscaling.Add(mgr) to install this Controller
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVerticalPodAutoscaler{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vpa-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		glog.Errorf("New controller: %v", err)
		return err
	}

	// Watch for changes to VerticalPodAutoscaler
	glog.V(4).Info("Watch VerticalPodAutoscaler.")
	err = c.Watch(&source.Kind{Type: &v1alpha1.VerticalPodAutoscaler{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		glog.Errorf("Watch changes to vpa: %v", err)
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileVerticalPodAutoscaler{}

// ReconcileVerticalPodAutoscaler reconciles a VerticalPodAutoscaler object
type ReconcileVerticalPodAutoscaler struct {
	client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a VerticalPodAutoscaler object and makes changes based on the state read
// and what is in the VerticalPodAutoscaler.Spec
//
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.suning.com,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileVerticalPodAutoscaler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the VerticalPodAutoscaler instance
	glog.V(4).Infof("Fetch the VerticalPodAutoscaler: %s", request.NamespacedName)

	vpa := &v1alpha1.VerticalPodAutoscaler{}
	err := r.Get(context.TODO(), request.NamespacedName, vpa)
	if err != nil {
		glog.Errorf("Get VerticalPodAutoscaler %s: %v", request.NamespacedName, err)
		return reconcile.Result{}, err
	}

	// only if the deploymentUpdated is false, then keep on updating
	if vpa.Status.DeploymentUpdated {
		glog.Errorf("vpa DeploymentUpdated need to be false")
		return reconcile.Result{}, nil
	}

	ipSet := vpa.Spec.IPSet
	if len(ipSet) == 0 {
		glog.Errorf("IPSet can't be empty")
		return reconcile.Result{}, fmt.Errorf("IPSet can't be empty")
	}

	glog.Info("Start to Update.")

	deployment := &appsv1.Deployment{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: vpa.Spec.DeploymentName, Namespace: vpa.Namespace}, deployment)
	if err != nil {
		glog.Errorf("Get deployment: %v", err)
		return reconcile.Result{}, err
	}

	// List ReplicaSets owned by this Deployment, while reconciling ControllerRef through adoption/orphaning.
	rsList, err := r.getReplicaSetsForDeployment(deployment)
	if err != nil {
		glog.Errorf("List ReplicaSets for Deployment: %v", err)
		return reconcile.Result{}, err
	}

	sort.Sort(util.ReplicaSetsByCreationTimestamp(rsList))

	// List all Pods owned by this Deployment, grouped by their ReplicaSet.
	podMap, err := r.getPodMapForDeployment(deployment, rsList)
	if err != nil {
		glog.Errorf("List Pods for Deployment: %v", err)
		return reconcile.Result{}, err
	}

	ipMap := make(map[string]interface{})
	for _, ip := range ipSet {
		ipMap[ip] = nil
	}

	// podUpgradeCount tells the deployment that there are several pods that need to be delete or updated
	podUpgradeCount, err := r.updatePodForDeployment(vpa, podMap, ipMap)
	if err != nil {
		glog.Errorf("Update Pod for Deployment: %v", err)
		return reconcile.Result{}, err
	}
	if podUpgradeCount == 0 {
		glog.Errorf("podUpgradeCount is zero")
		return reconcile.Result{}, fmt.Errorf("podUpgradeCount is zero")
	}

	glog.Infof("%d pods need to be deleted or upgraded", podUpgradeCount)

	switch vpa.Spec.Strategy.Type {
	case v1alpha1.PodUpgradeStrategyType:
		scaleTimestamp := vpa.Spec.ScaleTimestamp
		if len(scaleTimestamp) > 0 {
			glog.V(4).Info("Enter a timer task.")

			t, err := time.ParseInLocation(Layout, scaleTimestamp, time.Local)
			if err != nil {
				glog.Errorf("Time parse failed: %v", err)
				return reconcile.Result{}, err
			}
			now := time.Now()
			if t.Before(now) {
				glog.Errorf("ScaleTimestamp %s is before now time %s", t.String(), now.String())
				return reconcile.Result{}, fmt.Errorf("scaleTimestamp %s is before now time %s", t.String(), now.String())
			}

			go time.AfterFunc(t.Sub(now), func() {
				err := r.upgradeDeployment(vpa, deployment, podUpgradeCount)
				if err != nil {
					glog.Errorf("Time task failed: %v", err)
				}
			})
			return reconcile.Result{}, nil
		}
		err = r.upgradeDeployment(vpa, deployment, podUpgradeCount)
		if err != nil {
			glog.Errorf("Update vpa: %v", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	case v1alpha1.PodDeleteStrategyType:
		conditions := vpa.Status.Conditions
		condition := v1alpha1.VerticalPodAutoscalerCondition{
			LastUpdateTime: metav1.Time{
				Time: time.Now(),
			},
		}

		if vpa.Spec.Strategy.Phase == v1alpha1.Deleting {
			// adjust deployment replicas and deployment controller will watch this change.
			*(deployment.Spec.Replicas) -= int32(podUpgradeCount)
			err := r.Update(context.TODO(), deployment)
			if err != nil {
				glog.Errorf("Update deployment %s/%s replicas: %v", deployment.Namespace, deployment.Name, err)
				return reconcile.Result{}, err
			}
			condition.Reason = "DeploymentUpdated"
			condition.Message = fmt.Sprintf("Adjust deployment \"%s/%s\" replicas.", deployment.Namespace, deployment.Name)
		} else {
			condition.Reason = "PodUpdated"
			condition.Message = fmt.Sprintf("Pod has be binded.")
		}

		conditions = append(conditions, condition)

		vpa.Status.Conditions = conditions
		// if deployment is updated, update vpa status
		vpa.Status.DeploymentUpdated = true
		err = r.Update(context.TODO(), vpa)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, fmt.Errorf("unexpected vpa strategy type: %s", vpa.Spec.Strategy.Type)
}

// upgradeDeployment use to update the image and resource of deployment
// according to the given container name and tag annotation on deployment.
// Tag annotation use to specify which pod needs to be upgraded.
func (r *ReconcileVerticalPodAutoscaler) upgradeDeployment(vpa *v1alpha1.VerticalPodAutoscaler, deployment *appsv1.Deployment, podUpgradeCount int) error {
	// add annotation to deployment and update deployment container
	deployment.Annotations[GrayscaleAnnotation] = fmt.Sprintf("%d", podUpgradeCount)

	// update the container image and resource
	containers := deployment.Spec.Template.Spec.Containers
	for i := range containers {
		for _, resourceContainer := range vpa.Spec.Resources.Containers {
			if containers[i].Name != resourceContainer.ContainerName {
				continue
			}
			if len(resourceContainer.Image) > 0 {
				containers[i].Image = resourceContainer.Image
			}
			if resourceContainer.Resources != nil {
				containers[i].Resources = *resourceContainer.Resources
			}
		}
	}
	deployment.Spec.Template.Spec.Containers = containers

	err := r.Update(context.TODO(), deployment)
	if err != nil {
		return err
	}

	// if deployment is updated, update vpa status
	vpa.Status.DeploymentUpdated = true
	conditions := vpa.Status.Conditions
	condition := v1alpha1.VerticalPodAutoscalerCondition{
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
		Message: fmt.Sprintf("Deployment \"%s/%s\" is updated.", deployment.Namespace, deployment.Name),
		Reason:  "DeploymentUpdated",
	}
	conditions = append(conditions, condition)
	vpa.Status.Conditions = conditions
	err = r.Update(context.TODO(), vpa)
	if err != nil {
		return err
	}
	return nil
}

// getReplicaSetsForDeployment uses ControllerRefManager to reconcile
// ControllerRef by adopting and orphaning.
// It returns the list of ReplicaSets that this Deployment should manage.
func (r *ReconcileVerticalPodAutoscaler) getReplicaSetsForDeployment(d *appsv1.Deployment) ([]*appsv1.ReplicaSet, error) {
	// List all ReplicaSets to find those we own but that no longer match our selector.
	rsList := &appsv1.ReplicaSetList{}
	deploymentSelector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("deployment %s/%s has invalid label selector: %v", d.Namespace, d.Name, err)
	}

	err = r.List(context.TODO(), &client.ListOptions{
		LabelSelector: deploymentSelector,
		Namespace:     d.Namespace,
	}, rsList)
	if err != nil {
		return nil, err
	}

	sets := make([]*appsv1.ReplicaSet, 0)
	for i := range rsList.Items {
		sets = append(sets, &rsList.Items[i])
	}

	canAdoptFunc := util.RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh := &appsv1.Deployment{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, fresh)
		if err != nil {
			return nil, err
		}
		if fresh.UID != d.UID {
			return nil, fmt.Errorf("original Deployment %v/%v is gone: got uid %v, wanted %v", d.Namespace, d.Name, fresh.UID, d.UID)
		}
		return fresh, nil
	})
	cm := util.NewReplicaSetControllerRefManager(d, canAdoptFunc)
	return cm.ClaimReplicaSets(sets)
}

// getPodsForDeployment returns the Pods managed by a Deployment.
func (r *ReconcileVerticalPodAutoscaler) getPodMapForDeployment(d *appsv1.Deployment, rsList []*appsv1.ReplicaSet) (map[types.UID]*v1.PodList, error) {
	// Get all Pods that potentially belong to this Deployment.
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil, err
	}

	pods := &v1.PodList{}
	err = r.List(context.TODO(), &client.ListOptions{
		LabelSelector: selector,
		Namespace:     d.Namespace,
	}, pods)
	if err != nil {
		return nil, err
	}

	// Group Pods by their controller (if it's in rsList).
	podMap := make(map[types.UID]*v1.PodList, len(rsList))
	for _, rs := range rsList {
		podMap[rs.UID] = &v1.PodList{}
	}
	for _, pod := range pods.Items {
		// Do not ignore inactive Pods because Recreate Deployments need to verify that no
		// Pods from older versions are running before spinning up new Pods.
		controllerRef := metav1.GetControllerOf(&pod)
		if controllerRef == nil {
			continue
		}
		// Only append if we care about this UID.
		if podList, ok := podMap[controllerRef.UID]; ok {
			podList.Items = append(podList.Items, pod)
		}
	}
	return podMap, nil
}

// updatePodForDeployment tag annotation for pod via ipMap, if the podIP is not in ipMap, it's annotation need to be deleted.
func (r *ReconcileVerticalPodAutoscaler) updatePodForDeployment(vpa *v1alpha1.VerticalPodAutoscaler, podMap map[types.UID]*v1.PodList, ipMap map[string]interface{}) (int, error) {
	podUpgradeCount := 0
	strategy := 0
	var latestUID types.UID
	for uid, podList := range podMap {
		if podList != nil {
			strategy++
			latestUID = uid
		}
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.GetAnnotations() == nil {
				pod.Annotations = make(map[string]string)
			}
			_, ipOk := ipMap[pod.Status.PodIP]
			_, annotationOk := pod.Annotations[ScaleDownAnnotation]
			if !ipOk && annotationOk {
				delete(pod.Annotations, ScaleDownAnnotation)
				err := r.Update(context.TODO(), pod)
				if err != nil {
					return 0, err
				}
			}
			if !ipOk {
				continue
			}
			if !annotationOk {
				pod.Annotations[ScaleDownAnnotation] = "true"
				err := r.Update(context.TODO(), pod)
				if err != nil {
					return 0, err
				}
			}
			podUpgradeCount++
		}
	}
	if strategy > 1 {
		if vpa.Spec.Strategy.Type == v1alpha1.PodUpgradeStrategyType {
			return podUpgradeCount + len(podMap[latestUID].Items), nil
		}
	}
	return podUpgradeCount, nil
}
