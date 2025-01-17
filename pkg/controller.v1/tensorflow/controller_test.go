// Copyright 2018 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package controller provides a Kubernetes controller for a TFJob resource.
package tensorflow

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	batchv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned"

	commonv1 "github.com/kubeflow/common/pkg/apis/common/v1"
	"github.com/kubeflow/common/pkg/controller.v1/control"
	"github.com/kubeflow/tf-operator/cmd/tf-operator.v1/app/options"
	tfv1 "github.com/kubeflow/tf-operator/pkg/apis/tensorflow/v1"
	tfjobclientset "github.com/kubeflow/tf-operator/pkg/client/clientset/versioned"
	tfjobinformers "github.com/kubeflow/tf-operator/pkg/client/informers/externalversions"
	"github.com/kubeflow/tf-operator/pkg/common/util/v1/testutil"
)

var (
	tfJobRunning   = commonv1.JobRunning
	tfJobSucceeded = commonv1.JobSucceeded
)

func newTFController(
	config *rest.Config,
	kubeClientSet kubeclientset.Interface,
	volcanoClientSet volcanoclient.Interface,
	tfJobClientSet tfjobclientset.Interface,
	duration time.Duration,
	option options.ServerOption,
) (
	*TFController,
	kubeinformers.SharedInformerFactory, tfjobinformers.SharedInformerFactory,
) {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClientSet, duration)
	tfJobInformerFactory := tfjobinformers.NewSharedInformerFactory(tfJobClientSet, duration)

	tfJobInformer := NewUnstructuredTFJobInformer(config, metav1.NamespaceAll, time.Hour*12)

	ctr := NewTFController(tfJobInformer, kubeClientSet,
		volcanoClientSet, tfJobClientSet, kubeInformerFactory,
		tfJobInformerFactory, option)
	ctr.PodControl = &control.FakePodControl{}
	ctr.ServiceControl = &control.FakeServiceControl{}
	return ctr, kubeInformerFactory, tfJobInformerFactory
}

func TestNormalPath(t *testing.T) {
	testCases := map[string]struct {
		worker int
		ps     int

		// pod setup
		// ControllerError error
		// jobKeyForget    bool

		pendingWorkerPods   int32
		activeWorkerPods    int32
		succeededWorkerPods int32
		failedWorkerPods    int32

		pendingPSPods   int32
		activePSPods    int32
		succeededPSPods int32
		failedPSPods    int32

		activeWorkerServices int32
		activePSServices     int32

		// expectations
		expectedPodCreations     int32
		expectedPodDeletions     int32
		expectedServiceCreations int32

		expectedActiveWorkerPods    int32
		expectedSucceededWorkerPods int32
		expectedFailedWorkerPods    int32

		expectedActivePSPods    int32
		expectedSucceededPSPods int32
		expectedFailedPSPods    int32

		expectedCondition       *commonv1.JobConditionType
		expectedConditionReason string

		// There are some cases that should not check start time since the field should be set in the previous sync loop.
		needCheckStartTime bool
	}{
		"Local TFJob is created": {
			1, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0,
			1, 0, 1,
			0, 0, 0,
			0, 0, 0,
			// We can not check if it is created since the condition is set in addTFJob.
			nil, "",
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is created": {
			4, 2,
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0,
			6, 0, 6,
			0, 0, 0,
			0, 0, 0,
			nil, "",
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is created and all replicas are pending": {
			4, 2,
			4, 0, 0, 0,
			2, 0, 0, 0,
			4, 2,
			0, 0, 0,
			0, 0, 0,
			0, 0, 0,
			nil, "",
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is created and all replicas are running": {
			4, 2,
			0, 4, 0, 0,
			0, 2, 0, 0,
			4, 2,
			0, 0, 0,
			4, 0, 0,
			2, 0, 0,
			&tfJobRunning, tfJobRunningReason,
			true,
		},
		"Distributed TFJob (4 workers, 2 PS) is created, 2 workers, 1 PS are pending": {
			4, 2,
			2, 0, 0, 0,
			1, 0, 0, 0,
			2, 1,
			3, 0, 3,
			0, 0, 0,
			0, 0, 0,
			nil, "",
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is created, 2 workers, 1 PS are pending, 1 worker is running": {
			4, 2,
			2, 1, 0, 0,
			1, 0, 0, 0,
			3, 1,
			2, 0, 2,
			1, 0, 0,
			0, 0, 0,
			&tfJobRunning, tfJobRunningReason,
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is created, 2 workers, 1 PS are pending, 1 worker is succeeded": {
			4, 2,
			2, 0, 1, 0,
			1, 0, 0, 0,
			3, 1,
			2, 0, 2,
			0, 1, 0,
			0, 0, 0,
			nil, "",
			false,
		},
		"Distributed TFJob (4 workers, 2 PS) is succeeded": {
			4, 2,
			0, 0, 4, 0,
			0, 0, 2, 0,
			4, 2,
			0, 0, 0,
			0, 4, 0,
			0, 2, 0,
			&tfJobSucceeded, tfJobSucceededReason,
			false,
		},
	}

	for name, tc := range testCases {
		// Prepare the clientset and controller for the test.
		kubeClientSet := kubeclientset.NewForConfigOrDie(&rest.Config{
			Host: "",
			ContentConfig: rest.ContentConfig{
				GroupVersion: &v1.SchemeGroupVersion,
			},
		},
		)

		// Prepare the volcano clientset and controller for the test.
		volcanoClientSet := volcanoclient.NewForConfigOrDie(&rest.Config{
			Host: "",
			ContentConfig: rest.ContentConfig{
				GroupVersion: &batchv1beta1.SchemeGroupVersion,
			},
		},
		)

		config := &rest.Config{
			Host: "",
			ContentConfig: rest.ContentConfig{
				GroupVersion: &tfv1.GroupVersion,
			},
		}
		option := options.ServerOption{}
		tfJobClientSet := tfjobclientset.NewForConfigOrDie(config)
		ctr, kubeInformerFactory, _ := newTFController(config, kubeClientSet, volcanoClientSet, tfJobClientSet, 0, option)
		ctr.tfJobInformerSynced = testutil.AlwaysReady
		ctr.PodInformerSynced = testutil.AlwaysReady
		ctr.ServiceInformerSynced = testutil.AlwaysReady
		tfJobIndexer := ctr.tfJobInformer.GetIndexer()

		// Run the test logic.
		tfJob := testutil.NewTFJob(tc.worker, tc.ps)
		unstructured, err := testutil.ConvertTFJobToUnstructured(tfJob)
		if err != nil {
			t.Errorf("Failed to convert the TFJob to Unstructured: %v", err)
		}

		if err := tfJobIndexer.Add(unstructured); err != nil {
			t.Errorf("Failed to add tfjob to tfJobIndexer: %v", err)
		}

		podIndexer := kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()
		testutil.SetPodsStatuses(podIndexer, tfJob, testutil.LabelWorker, tc.pendingWorkerPods, tc.activeWorkerPods, tc.succeededWorkerPods, tc.failedWorkerPods, nil, t)
		testutil.SetPodsStatuses(podIndexer, tfJob, testutil.LabelPS, tc.pendingPSPods, tc.activePSPods, tc.succeededPSPods, tc.failedPSPods, nil, t)

		serviceIndexer := kubeInformerFactory.Core().V1().Services().Informer().GetIndexer()
		testutil.SetServices(serviceIndexer, tfJob, testutil.LabelWorker, tc.activeWorkerServices, t)
		testutil.SetServices(serviceIndexer, tfJob, testutil.LabelPS, tc.activePSServices, t)

		//_, err = ctr.syncTFJob(testutil.GetKey(tfJob, t))
		_ = ctr.ReconcileJobs(tfJob, tfJob.Spec.TFReplicaSpecs, tfJob.Status, &tfJob.Spec.RunPolicy)

		fakePodControl := ctr.PodControl.(*control.FakePodControl)
		fakeServiceControl := ctr.ServiceControl.(*control.FakeServiceControl)
		if int32(len(fakePodControl.Templates)) != tc.expectedPodCreations {
			t.Errorf("%s: unexpected number of pod creates.  Expected %d, saw %d\n", name, tc.expectedPodCreations, len(fakePodControl.Templates))
		}
		if int32(len(fakeServiceControl.Templates)) != tc.expectedServiceCreations {
			t.Errorf("%s: unexpected number of service creates.  Expected %d, saw %d\n", name, tc.expectedServiceCreations, len(fakeServiceControl.Templates))
		}
		if int32(len(fakePodControl.DeletePodName)) != tc.expectedPodDeletions {
			t.Errorf("%s: unexpected number of pod deletes.  Expected %d, saw %d\n", name, tc.expectedPodDeletions, len(fakePodControl.DeletePodName))
		}
		// Each create should have an accompanying ControllerRef.
		if len(fakePodControl.ControllerRefs) != int(tc.expectedPodCreations) {
			t.Errorf("%s: unexpected number of ControllerRefs.  Expected %d, saw %d\n", name, tc.expectedPodCreations, len(fakePodControl.ControllerRefs))
		}
		// Make sure the ControllerRefs are correct.
		for _, controllerRef := range fakePodControl.ControllerRefs {
			if got, want := controllerRef.APIVersion, tfv1.GroupVersion.String(); got != want {
				t.Errorf("controllerRef.APIVersion = %q, want %q", got, want)
			}
			if got, want := controllerRef.Kind, tfv1.Kind; got != want {
				t.Errorf("controllerRef.Kind = %q, want %q", got, want)
			}
			if got, want := controllerRef.Name, tfJob.Name; got != want {
				t.Errorf("controllerRef.Name = %q, want %q", got, want)
			}
			if got, want := controllerRef.UID, tfJob.UID; got != want {
				t.Errorf("controllerRef.UID = %q, want %q", got, want)
			}
			if controllerRef.Controller == nil || !*controllerRef.Controller {
				t.Errorf("controllerRef.Controller is not set to true")
			}
		}
		// Validate worker status.
		if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)] != nil {
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Active != tc.expectedActiveWorkerPods {
				t.Errorf("%s: unexpected number of active pods.  Expected %d, saw %d\n",
					name, tc.expectedActiveWorkerPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Active)
			}
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Succeeded != tc.expectedSucceededWorkerPods {
				t.Errorf("%s: unexpected number of succeeded pods.  Expected %d, saw %d\n",
					name, tc.expectedSucceededWorkerPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Succeeded)
			}
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Failed != tc.expectedFailedWorkerPods {
				t.Errorf("%s: unexpected number of failed pods.  Expected %d, saw %d\n",
					name, tc.expectedFailedWorkerPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypeWorker)].Failed)
			}
		}
		// Validate PS status.
		if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)] != nil {
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Active != tc.expectedActivePSPods {
				t.Errorf("%s: unexpected number of active pods.  Expected %d, saw %d\n",
					name, tc.expectedActivePSPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Active)
			}
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Succeeded != tc.expectedSucceededPSPods {
				t.Errorf("%s: unexpected number of succeeded pods.  Expected %d, saw %d\n",
					name, tc.expectedSucceededPSPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Succeeded)
			}
			if tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Failed != tc.expectedFailedPSPods {
				t.Errorf("%s: unexpected number of failed pods.  Expected %d, saw %d\n",
					name, tc.expectedFailedPSPods,
					tfJob.Status.ReplicaStatuses[commonv1.ReplicaType(tfv1.TFReplicaTypePS)].Failed)
			}
		}
		// Validate StartTime.
		if tc.needCheckStartTime && tfJob.Status.StartTime == nil {
			t.Errorf("%s: StartTime was not set", name)
		}
		// Validate conditions.
		if tc.expectedCondition != nil && !testutil.CheckCondition(tfJob, *tc.expectedCondition, tc.expectedConditionReason) {
			t.Errorf("%s: expected condition %#v, got %#v", name, *tc.expectedCondition, tfJob.Status.Conditions)
		}
	}
}

func TestRun(t *testing.T) {
	// Prepare the clientset and controller for the test.
	kubeClientSet := kubeclientset.NewForConfigOrDie(&rest.Config{
		Host: "",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &v1.SchemeGroupVersion,
		},
	},
	)

	// Prepare the volcano clientset and controller for the test.
	volcanoClientSet := volcanoclient.NewForConfigOrDie(&rest.Config{
		Host: "",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &batchv1beta1.SchemeGroupVersion,
		},
	},
	)

	config := &rest.Config{
		Host: "",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &tfv1.GroupVersion,
		},
	}
	tfJobClientSet := tfjobclientset.NewForConfigOrDie(config)
	ctr, _, _ := newTFController(config, kubeClientSet,
		volcanoClientSet, tfJobClientSet, 0, options.ServerOption{})
	ctr.tfJobInformerSynced = testutil.AlwaysReady
	ctr.PodInformerSynced = testutil.AlwaysReady
	ctr.ServiceInformerSynced = testutil.AlwaysReady

	stopCh := make(chan struct{})
	go func() {
		// It is a hack to let the controller stop to run without errors.
		// We can not just send a struct to stopCh because there are multiple
		// receivers in controller.Run.
		time.Sleep(testutil.SleepInterval)
		stopCh <- struct{}{}
	}()
	err := ctr.Run(testutil.ThreadCount, stopCh)
	if err != nil {
		t.Errorf("Failed to run: %v", err)
	}
}
