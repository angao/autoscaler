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

package util

import (
	"fmt"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// ReplicaSetControllerRefManager is used to manage controllerRef of ReplicaSets.
type ReplicaSetControllerRefManager struct {
	Controller metav1.Object

	canAdoptErr  error
	canAdoptOnce sync.Once
	CanAdoptFunc func() error
}

// NewReplicaSetControllerRefManager returns a ReplicaSetControllerRefManager that exposes
// methods to manage the controllerRef of ReplicaSets.
//
// The CanAdopt() function can be used to perform a potentially expensive check
// (such as a live GET from the API server) prior to the first adoption.
// It will only be called (at most once) if an adoption is actually attempted.
// If CanAdopt() returns a non-nil error, all adoptions will fail.
//
// NOTE: Once CanAdopt() is called, it will not be called again by the same
//       ReplicaSetControllerRefManager instance. Create a new instance if it
//       makes sense to check CanAdopt() again (e.g. in a different sync pass).
func NewReplicaSetControllerRefManager(
	controller metav1.Object,
	canAdopt func() error,
) *ReplicaSetControllerRefManager {
	return &ReplicaSetControllerRefManager{
		Controller:   controller,
		CanAdoptFunc: canAdopt,
	}
}

func (m *ReplicaSetControllerRefManager) CanAdopt() error {
	m.canAdoptOnce.Do(func() {
		if m.CanAdoptFunc != nil {
			m.canAdoptErr = m.CanAdoptFunc()
		}
	})
	return m.canAdoptErr
}

// ClaimReplicaSets tries to take ownership of a list of ReplicaSets.
func (m *ReplicaSetControllerRefManager) ClaimReplicaSets(sets []*appsv1.ReplicaSet) ([]*appsv1.ReplicaSet, error) {
	var claimed []*appsv1.ReplicaSet
	var errList []error

	adopt := func(obj metav1.Object) error {
		return m.AdoptReplicaSet(obj.(*appsv1.ReplicaSet))
	}

	for _, rs := range sets {
		ok, err := m.ClaimObject(rs, adopt)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		if ok {
			claimed = append(claimed, rs)
		}
	}
	return claimed, utilerrors.NewAggregate(errList)
}

func (m *ReplicaSetControllerRefManager) ClaimObject(obj metav1.Object, adopt func(metav1.Object) error) (bool, error) {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef != nil {
		if controllerRef.UID != m.Controller.GetUID() {
			return false, nil
		}

		if m.Controller.GetDeletionTimestamp() != nil {
			return false, nil
		}
		return true, nil
	}

	if m.Controller.GetDeletionTimestamp() != nil {
		return false, nil
	}
	if obj.GetDeletionTimestamp() != nil {
		return false, nil
	}
	return true, nil
}

func (m *ReplicaSetControllerRefManager) AdoptReplicaSet(rs *appsv1.ReplicaSet) error {
	if err := m.CanAdopt(); err != nil {
		return fmt.Errorf("can't adopt ReplicaSet %v/%v (%v): %v", rs.Namespace, rs.Name, rs.UID, err)
	}
	return nil
}

// RecheckDeletionTimestamp returns a CanAdopt() function to recheck deletion.
//
// The CanAdopt() function calls getObject() to fetch the latest value,
// and denies adoption attempts if that object has a non-nil DeletionTimestamp.
func RecheckDeletionTimestamp(getObject func() (metav1.Object, error)) func() error {
	return func() error {
		obj, err := getObject()
		if err != nil {
			return fmt.Errorf("can't recheck DeletionTimestamp: %v", err)
		}
		if obj.GetDeletionTimestamp() != nil {
			return fmt.Errorf("%v/%v has just been deleted at %v", obj.GetNamespace(), obj.GetName(), obj.GetDeletionTimestamp())
		}
		return nil
	}
}

// ReplicaSetsByCreationTimestamp sorts a list of ReplicaSet by creation timestamp, using their names as a tie breaker.
type ReplicaSetsByCreationTimestamp []*appsv1.ReplicaSet

func (o ReplicaSetsByCreationTimestamp) Len() int      { return len(o) }
func (o ReplicaSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ReplicaSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}
