/*
Copyright 2017 The Kubernetes Authors.

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

package cache

import (
	"k8s.io/api/core/v1"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

// Cache collects pods/nodes/queues information
// and provides information snapshot
type Cache interface {
	// Run start informer
	Run(stopCh <-chan struct{})

	// Snapshot deep copy overall cache information into snapshot
	Snapshot() *api.ClusterInfo

	// WaitForCacheSync waits for all cache synced
	WaitForCacheSync(stopCh <-chan struct{}) bool

	// Bind binds Task to the target host.
	// TODO(jinzhej): clean up expire Tasks.
	Bind(task *api.TaskInfo, hostname string) error

	// Evict evicts the task to release resources.
	Evict(task *api.TaskInfo, reason string) error

	// RecordJobStatusEvent records related events according to job status.
	// Deprecated: remove it after removed PDB support.
	RecordJobStatusEvent(job *api.JobInfo)

	// UpdateJobStatus puts job in backlog for a while.
	UpdateJobStatus(job *api.JobInfo) (*api.JobInfo, error)

	// AllocateVolumes allocates volume on the host to the task
	AllocateVolumes(task *api.TaskInfo, hostname string) error

	// BindVolumes binds volumes to the task
	BindVolumes(task *api.TaskInfo) error
}

type VolumeBinder interface {
	AllocateVolumes(task *api.TaskInfo, hostname string) error
	BindVolumes(task *api.TaskInfo) error
}

type Binder interface {
	Bind(task *v1.Pod, hostname string) error
}

type Evictor interface {
	Evict(pod *v1.Pod) error
}

// StatusUpdater updates pod with given PodCondition
type StatusUpdater interface {
	UpdatePodCondition(pod *v1.Pod, podCondition *v1.PodCondition) (*v1.Pod, error)
	UpdatePodGroup(pg *v1alpha1.PodGroup) (*v1alpha1.PodGroup, error)
}
