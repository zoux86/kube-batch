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

package gang

import (
	"fmt"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/metrics"
)

type gangPlugin struct {
	// Arguments given for the plugin
	pluginArguments map[string]string
}

func New(arguments map[string]string) framework.Plugin {
	return &gangPlugin{pluginArguments: arguments}
}

func (gp *gangPlugin) Name() string {
	return "gang"
}

// readyTaskNum return the number of tasks that are ready to run.
func readyTaskNum(job *api.JobInfo) int32 {
	occupid := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) ||
			status == api.Succeeded ||
			status == api.Pipelined {
			occupid = occupid + len(tasks)
		}
	}

	return int32(occupid)
}

// validTaskNum return the number of tasks that are valid.
func validTaskNum(job *api.JobInfo) int32 {
	occupied := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) ||
			status == api.Succeeded ||
			status == api.Pipelined ||
			status == api.Pending {
			occupied = occupied + len(tasks)
		}
	}

	return int32(occupied)
}

func jobReady(obj interface{}) bool {
	job := obj.(*api.JobInfo)

	occupied := readyTaskNum(job)

	return occupied >= job.MinAvailable
}

func (gp *gangPlugin) OnSessionOpen(ssn *framework.Session) {
	validJobFn := func(obj interface{}) *api.ValidateResult {
		job, ok := obj.(*api.JobInfo)
		if !ok {
			return &api.ValidateResult{
				Pass:    false,
				Message: fmt.Sprintf("Failed to convert <%v> to *JobInfo", obj),
			}
		}

		vtn := validTaskNum(job)
		if vtn < job.MinAvailable {
			return &api.ValidateResult{
				Pass:   false,
				Reason: v1alpha1.NotEnoughPodsReason,
				Message: fmt.Sprintf("Not enough valid tasks for gang-scheduling, valid: %d, min: %d",
					vtn, job.MinAvailable),
			}
		}
		return nil
	}

	ssn.AddJobValidFn(gp.Name(), validJobFn)

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
		var victims []*api.TaskInfo

		for _, preemptee := range preemptees {
			job := ssn.Jobs[preemptee.Job]
			occupid := readyTaskNum(job)
			preemptable := job.MinAvailable <= occupid-1 || job.MinAvailable == 1

			if !preemptable {
				glog.V(3).Infof("Can not preempt task <%v/%v> because of gang-scheduling",
					preemptee.Namespace, preemptee.Name)
			} else {
				victims = append(victims, preemptee)
			}
		}

		glog.V(3).Infof("Victims from Gang plugins are %+v", victims)

		return victims
	}

	// TODO(k82cn): Support preempt/reclaim batch job.
	ssn.AddReclaimableFn(gp.Name(), preemptableFn)
	ssn.AddPreemptableFn(gp.Name(), preemptableFn)

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		lReady := jobReady(lv)
		rReady := jobReady(rv)

		glog.V(4).Infof("Gang JobOrderFn: <%v/%v> is ready: %t, <%v/%v> is ready: %t",
			lv.Namespace, lv.Name, lReady, rv.Namespace, rv.Name, rReady)

		if lReady && rReady {
			return 0
		}

		if lReady {
			return 1
		}

		if rReady {
			return -1
		}

		return 0
	}

	ssn.AddJobOrderFn(gp.Name(), jobOrderFn)
	ssn.AddJobReadyFn(gp.Name(), jobReady)
}

func (gp *gangPlugin) OnSessionClose(ssn *framework.Session) {
	var unreadyTaskCount int32
	var unScheduleJobCount int
	for _, job := range ssn.Jobs {
		if !jobReady(job) {
			unreadyTaskCount = job.MinAvailable - readyTaskNum(job)
			msg := fmt.Sprintf("%v/%v tasks in gang unschedulable: %v",
				job.MinAvailable-readyTaskNum(job), len(job.Tasks), job.FitError())

			unScheduleJobCount += 1
			metrics.UpdateUnscheduleTaskCount(job.Name, int(unreadyTaskCount))
			metrics.RegisterJobRetries(job.Name)

			jc := &v1alpha1.PodGroupCondition{
				Type:               v1alpha1.PodGroupUnschedulableType,
				Status:             v1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				TransitionID:       string(ssn.UID),
				Reason:             v1alpha1.NotEnoughResourcesReason,
				Message:            msg,
			}

			if err := ssn.UpdateJobCondition(job, jc); err != nil {
				glog.Errorf("Failed to update job <%s/%s> condition: %v",
					job.Namespace, job.Name, err)
			}
		}
	}

	metrics.UpdateUnscheduleJobCount(unScheduleJobCount)
}
