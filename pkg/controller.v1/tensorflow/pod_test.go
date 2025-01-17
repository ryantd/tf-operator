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
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	batchv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned"

	commonv1 "github.com/kubeflow/common/pkg/apis/common/v1"
	"github.com/kubeflow/common/pkg/controller.v1/common"
	"github.com/kubeflow/common/pkg/controller.v1/control"
	"github.com/kubeflow/common/pkg/controller.v1/expectation"
	"github.com/kubeflow/tf-operator/cmd/tf-operator.v1/app/options"
	tfv1 "github.com/kubeflow/tf-operator/pkg/apis/tensorflow/v1"
	tfjobclientset "github.com/kubeflow/tf-operator/pkg/client/clientset/versioned"
	"github.com/kubeflow/tf-operator/pkg/common/util/v1/testutil"
)

func TestAddPod(t *testing.T) {
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
	tfJobIndexer := ctr.tfJobInformer.GetIndexer()

	stopCh := make(chan struct{})
	run := func(<-chan struct{}) {
		if err := ctr.Run(testutil.ThreadCount, stopCh); err != nil {
			t.Errorf("Failed to run the controller: %v", err)
		}
	}
	go run(stopCh)

	var key string
	syncChan := make(chan string)
	ctr.syncHandler = func(tfJobKey string) (bool, error) {
		key = tfJobKey
		<-syncChan
		return true, nil
	}

	tfJob := testutil.NewTFJob(1, 0)
	unstructured, err := testutil.ConvertTFJobToUnstructured(tfJob)
	if err != nil {
		t.Errorf("Failed to convert the TFJob to Unstructured: %v", err)
	}

	if err := tfJobIndexer.Add(unstructured); err != nil {
		t.Errorf("Failed to add tfjob to tfJobIndexer: %v", err)
	}
	pod := testutil.NewPod(tfJob, testutil.LabelWorker, 0)
	ctr.AddPod(pod)

	syncChan <- "sync"
	if key != testutil.GetKey(tfJob, t) {
		t.Errorf("Failed to enqueue the TFJob %s: expected %s, got %s", tfJob.Name, testutil.GetKey(tfJob, t), key)
	}
	close(stopCh)
}

func TestExpectation(t *testing.T) {
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

	ctr.PodControl = &control.FakePodControl{}
	tfJob := testutil.NewTFJob(2, 1)

	var err error
	if err = ctr.createNewPod(tfJob, "worker", "0",
		tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker],
		false, tfJob.Spec.TFReplicaSpecs); err != nil {
		t.Errorf("Expected get nil, got error %v", err)
	}

	tfjobKey, err := KeyFunc(tfJob)
	if err != nil {
		t.Errorf("Expected nil, got error %v", err)
	}
	expectationPodsKey := expectation.GenExpectationPodsKey(tfjobKey, "worker")
	e, found, err := ctr.Expectations.GetExpectations(expectationPodsKey)
	if err != nil {
		t.Errorf("Expected nil, got error %v", err)
	}
	if !found {
		t.Errorf("Expected to get the corresponding expectation")
	}
	if add, del := e.GetExpectations(); add != 1 || del != 0 {
		t.Errorf("Expected get 1 add and 0 del, got %d add and %d del", add, del)
	}
}

func TestExpectationWithError(t *testing.T) {
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

	ctr.PodControl = &control.FakePodControl{}
	tfJob := testutil.NewTFJob(2, 1)

	// Fake an error.
	ctr.PodControl.(*control.FakePodControl).Err = fmt.Errorf("Fake")

	var err error
	if err = ctr.createNewPod(tfJob, "worker", "0",
		tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker],
		false, tfJob.Spec.TFReplicaSpecs); err == nil {
		t.Errorf("Expected error, got nil")
	}

	tfjobKey, err := KeyFunc(tfJob)
	if err != nil {
		t.Errorf("Expected nil, got error %v", err)
	}
	expectationPodsKey := expectation.GenExpectationPodsKey(tfjobKey, "worker")
	e, found, err := ctr.Expectations.GetExpectations(expectationPodsKey)
	if err != nil {
		t.Errorf("Expected nil, got error %v", err)
	}
	if !found {
		t.Errorf("Expected to get the corresponding expectation")
	}
	if add, del := e.GetExpectations(); add != 0 || del != 0 {
		t.Errorf("Expected get 0 add and 0 del, got %d add and %d del", add, del)
	}
}

func TestClusterSpec(t *testing.T) {
	type tc struct {
		tfJob               *tfv1.TFJob
		rt                  string
		index               string
		customClusterDomain string
		expectedClusterSpec string
	}
	testCase := []tc{
		tc{
			tfJob:               testutil.NewTFJobWithNamespace(1, 0, "ns0"),
			rt:                  "worker",
			index:               "0",
			customClusterDomain: "",
			expectedClusterSpec: "",
		},
		tc{
			tfJob:               testutil.NewTFJobWithNamespace(1, 0, "ns1"),
			rt:                  "worker",
			index:               "0",
			customClusterDomain: "tf.training.com",
			expectedClusterSpec: "",
		},
		tc{
			tfJob:               testutil.NewTFJobWithNamespace(1, 1, "ns2"),
			rt:                  "worker",
			index:               "0",
			customClusterDomain: "tf.training.org",
			expectedClusterSpec: `{"cluster":{"ps":["` + testutil.TestTFJobName +
				`-ps-0.ns2.svc.tf.training.org:2222"],"worker":["` + testutil.TestTFJobName +
				`-worker-0.ns2.svc.tf.training.org:2222"]},"task":{"type":"worker","index":0},"environment":"cloud"}`,
		},
		tc{
			tfJob:               testutil.NewTFJobWithEvaluatorAndNamespace(1, 1, 1, "ns3"),
			rt:                  "worker",
			index:               "0",
			customClusterDomain: "tf.training.io",
			expectedClusterSpec: `{"cluster":{"evaluator":["` + testutil.TestTFJobName +
				`-evaluator-0.ns3.svc.tf.training.io:2222"],"ps":["` + testutil.TestTFJobName +
				`-ps-0.ns3.svc.tf.training.io:2222"],"worker":["` + testutil.TestTFJobName +
				`-worker-0.ns3.svc.tf.training.io:2222"]},"task":{"type":"worker","index":0},"environment":"cloud"}`,
		},
		tc{
			tfJob:               testutil.NewTFJobWithEvaluatorAndNamespace(1, 1, 1, "ns3"),
			rt:                  "worker",
			index:               "0",
			customClusterDomain: "",
			expectedClusterSpec: `{"cluster":{"evaluator":["` + testutil.TestTFJobName +
				`-evaluator-0.ns3.svc:2222"],"ps":["` + testutil.TestTFJobName +
				`-ps-0.ns3.svc:2222"],"worker":["` + testutil.TestTFJobName +
				`-worker-0.ns3.svc:2222"]},"task":{"type":"worker","index":0},"environment":"cloud"}`,
		},
	}
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

	for _, c := range testCase {
		os.Setenv(EnvCustomClusterDomain, c.customClusterDomain)

		podTemplate := c.tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].Template.DeepCopy()

		// Set name for the template.
		podTemplate.Name = common.GenGeneralName(c.tfJob.GetName(), c.rt, c.index)

		if podTemplate.Labels == nil {
			podTemplate.Labels = make(map[string]string)
		}

		// Set type and index for the worker.
		labels := ctr.GenLabels(c.tfJob.GetName())
		labels[commonv1.ReplicaTypeLabel] = c.rt
		labels[commonv1.ReplicaIndexLabel] = c.index

		for key, value := range labels {
			podTemplate.Labels[key] = value
		}

		if err := ctr.SetClusterSpec(c.tfJob, podTemplate, c.rt, c.index); err != nil {
			t.Errorf("Failed to set cluster spec: %v", err)
		}
		// The expected cluster spec is nil, which means that we should not set TF_CONFIG.
		if c.expectedClusterSpec == "" {
			if len(podTemplate.Spec.Containers[0].Env) != 0 {
				t.Errorf("Expected empty TF_CONFIG, got %s",
					podTemplate.Spec.Containers[0].Env[0].Value)
			}
		} else {
			actual := podTemplate.Spec.Containers[0].Env[0].Value
			if c.expectedClusterSpec != actual {
				t.Errorf("Expected %s, got %s", c.expectedClusterSpec, actual)
			}
		}
	}
}

func TestIsDistributed(t *testing.T) {
	type tc struct {
		tfJob    *tfv1.TFJob
		expected bool
	}
	testCase := []tc{
		{
			tfJob:    testutil.NewTFJob(1, 0),
			expected: false,
		},
		{
			tfJob:    testutil.NewTFJob(1, 1),
			expected: true,
		},
		{
			tfJob:    testutil.NewTFJob(0, 1),
			expected: false,
		},
		{
			tfJob:    testutil.NewTFJobWithChief(1, 0),
			expected: true,
		},
	}
	for _, c := range testCase {
		actual := isDistributed(c.tfJob)
		if actual != c.expected {
			t.Errorf("Expected %t, got %t", c.expected, actual)
		}
	}
}

func TestRestartPolicy(t *testing.T) {
	type tc struct {
		tfJob                 *tfv1.TFJob
		expectedRestartPolicy v1.RestartPolicy
		expectedType          commonv1.ReplicaType
	}
	testCase := []tc{
		func() tc {
			tfJob := testutil.NewTFJob(1, 0)
			specRestartPolicy := commonv1.RestartPolicyExitCode
			tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].RestartPolicy = specRestartPolicy
			return tc{
				tfJob:                 tfJob,
				expectedRestartPolicy: v1.RestartPolicyNever,
				expectedType:          tfv1.TFReplicaTypeWorker,
			}
		}(),
		func() tc {
			tfJob := testutil.NewTFJob(1, 0)
			specRestartPolicy := commonv1.RestartPolicyNever
			tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].RestartPolicy = specRestartPolicy
			return tc{
				tfJob:                 tfJob,
				expectedRestartPolicy: v1.RestartPolicyNever,
				expectedType:          tfv1.TFReplicaTypeWorker,
			}
		}(),
		func() tc {
			tfJob := testutil.NewTFJob(1, 0)
			specRestartPolicy := commonv1.RestartPolicyAlways
			tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].RestartPolicy = specRestartPolicy
			return tc{
				tfJob:                 tfJob,
				expectedRestartPolicy: v1.RestartPolicyAlways,
				expectedType:          tfv1.TFReplicaTypeWorker,
			}
		}(),
		func() tc {
			tfJob := testutil.NewTFJob(1, 0)
			specRestartPolicy := commonv1.RestartPolicyOnFailure
			tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].RestartPolicy = specRestartPolicy
			return tc{
				tfJob:                 tfJob,
				expectedRestartPolicy: v1.RestartPolicyOnFailure,
				expectedType:          tfv1.TFReplicaTypeWorker,
			}
		}(),
	}
	for _, c := range testCase {
		spec := c.tfJob.Spec.TFReplicaSpecs[c.expectedType]
		podTemplate := spec.Template
		setRestartPolicy(&podTemplate, spec)
		if podTemplate.Spec.RestartPolicy != c.expectedRestartPolicy {
			t.Errorf("Expected %s, got %s", c.expectedRestartPolicy, podTemplate.Spec.RestartPolicy)
		}
	}
}

func TestExitCode(t *testing.T) {
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
	ctr, kubeInformerFactory, _ := newTFController(config, kubeClientSet,
		volcanoClientSet, tfJobClientSet, 0, options.ServerOption{})
	fakePodControl := &control.FakePodControl{}
	ctr.PodControl = fakePodControl
	ctr.tfJobInformerSynced = testutil.AlwaysReady
	ctr.PodInformerSynced = testutil.AlwaysReady
	ctr.ServiceInformerSynced = testutil.AlwaysReady
	tfJobIndexer := ctr.tfJobInformer.GetIndexer()
	podIndexer := kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()

	stopCh := make(chan struct{})
	run := func(<-chan struct{}) {
		if err := ctr.Run(testutil.ThreadCount, stopCh); err != nil {
			t.Errorf("Failed to run the controller: %v", err)
		}
	}
	go run(stopCh)

	tfJob := testutil.NewTFJob(1, 0)
	tfJob.Spec.TFReplicaSpecs[tfv1.TFReplicaTypeWorker].RestartPolicy = commonv1.RestartPolicyExitCode
	unstructured, err := testutil.ConvertTFJobToUnstructured(tfJob)
	if err != nil {
		t.Errorf("Failed to convert the TFJob to Unstructured: %v", err)
	}

	if err := tfJobIndexer.Add(unstructured); err != nil {
		t.Errorf("Failed to add tfjob to tfJobIndexer: %v", err)
	}
	pod := testutil.NewPod(tfJob, testutil.LabelWorker, 0)
	pod.Status.Phase = v1.PodFailed
	pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{})
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
		Name: tfv1.DefaultContainerName,
		State: v1.ContainerState{
			Terminated: &v1.ContainerStateTerminated{
				ExitCode: 130,
			},
		},
	})

	if err := podIndexer.Add(pod); err != nil {
		t.Errorf("%s: unexpected error when adding pod %v", tfJob.Name, err)
	}
	_ = ctr.ReconcileJobs(tfJob, tfJob.Spec.TFReplicaSpecs, tfJob.Status, &tfJob.Spec.RunPolicy)
	// _, err = ctr.syncTFJob(testutil.GetKey(tfJob, t))
	// if err != nil {
	// 	t.Errorf("%s: unexpected error when syncing jobs %v", tfJob.Name, err)
	// }

	found := false
	for _, deletedPodName := range fakePodControl.DeletePodName {
		if deletedPodName == pod.Name {
			found = true
		}
	}
	if !found {
		t.Errorf("Failed to delete pod %s", pod.Name)
	}
	close(stopCh)
}

// Test scaling down number of workers while training is running
func TestScaleDown(t *testing.T) {
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
	ctr, kubeInformerFactory, _ := newTFController(config, kubeClientSet,
		volcanoClientSet, tfJobClientSet, 0, options.ServerOption{})
	fakePodControl := &control.FakePodControl{}
	ctr.PodControl = fakePodControl
	ctr.Recorder = &record.FakeRecorder{}
	ctr.tfJobInformerSynced = testutil.AlwaysReady
	ctr.PodInformerSynced = testutil.AlwaysReady
	ctr.ServiceInformerSynced = testutil.AlwaysReady
	tfJobIndexer := ctr.tfJobInformer.GetIndexer()
	podIndexer := kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()

	stopCh := make(chan struct{})
	run := func(<-chan struct{}) {
		if err := ctr.Run(testutil.ThreadCount, stopCh); err != nil {
			t.Errorf("Failed to run the controller: %v", err)
		}
	}
	go run(stopCh)

	tfJob := testutil.NewTFJob(2, 0)
	tfJob.SelfLink = "/api/v1/namespaces/default/tfjob/test-tfjob"
	tfJob.Spec.EnableDynamicWorker = true
	unstructured, err := testutil.ConvertTFJobToUnstructured(tfJob)
	if err != nil {
		t.Errorf("Failed to convert the TFJob to Unstructured: %v", err)
	}

	if err := tfJobIndexer.Add(unstructured); err != nil {
		t.Errorf("Failed to add tfjob to tfJobIndexer: %v", err)
	}
	pod0 := testutil.NewPod(tfJob, testutil.LabelWorker, 0)
	pod1 := testutil.NewPod(tfJob, testutil.LabelWorker, 1)
	pod2 := testutil.NewPod(tfJob, testutil.LabelWorker, 2)

	if err := podIndexer.Add(pod0); err != nil {
		t.Errorf("%s: unexpected error when adding pod %v", tfJob.Name, err)
	}
	if err := podIndexer.Add(pod1); err != nil {
		t.Errorf("%s: unexpected error when adding pod %v", tfJob.Name, err)
	}
	if err := podIndexer.Add(pod2); err != nil {
		t.Errorf("%s: unexpected error when adding pod %v", tfJob.Name, err)
	}

	_ = ctr.ReconcileJobs(tfJob, tfJob.Spec.TFReplicaSpecs, tfJob.Status, &tfJob.Spec.RunPolicy)
	// _, err = ctr.syncTFJob(testutil.GetKey(tfJob, t))
	// if err != nil {
	// 	t.Errorf("%s: unexpected error when syncing jobs %v", tfJob.Name, err)
	// }

	expectedDeletePods := []string{"worker-2"}
	if !reflect.DeepEqual(expectedDeletePods, fakePodControl.DeletePodName) {
		t.Errorf("Scale down workers test failed")
	}
	close(stopCh)
}

// Test scaling up number of workers while training is running
func TestScaleUp(t *testing.T) {
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
	ctr, kubeInformerFactory, _ := newTFController(config, kubeClientSet,
		volcanoClientSet, tfJobClientSet, 0, options.ServerOption{})
	fakePodControl := &control.FakePodControl{}
	ctr.PodControl = fakePodControl
	ctr.tfJobInformerSynced = testutil.AlwaysReady
	ctr.PodInformerSynced = testutil.AlwaysReady
	ctr.ServiceInformerSynced = testutil.AlwaysReady
	tfJobIndexer := ctr.tfJobInformer.GetIndexer()
	podIndexer := kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()

	stopCh := make(chan struct{})
	run := func(<-chan struct{}) {
		if err := ctr.Run(testutil.ThreadCount, stopCh); err != nil {
			t.Errorf("Failed to run the controller: %v", err)
		}
	}
	go run(stopCh)

	tfJob := testutil.NewTFJob(3, 0)
	tfJob.Spec.EnableDynamicWorker = true
	unstructured, err := testutil.ConvertTFJobToUnstructured(tfJob)
	if err != nil {
		t.Errorf("Failed to convert the TFJob to Unstructured: %v", err)
	}

	if err := tfJobIndexer.Add(unstructured); err != nil {
		t.Errorf("Failed to add tfjob to tfJobIndexer: %v", err)
	}
	pod0 := testutil.NewPod(tfJob, testutil.LabelWorker, 0)

	if err := podIndexer.Add(pod0); err != nil {
		t.Errorf("%s: unexpected error when adding pod %v", tfJob.Name, err)
	}

	_ = ctr.ReconcileJobs(tfJob, tfJob.Spec.TFReplicaSpecs, tfJob.Status, &tfJob.Spec.RunPolicy)
	// _, err = ctr.syncTFJob(testutil.GetKey(tfJob, t))
	// if err != nil {
	// 	t.Errorf("%s: unexpected error when syncing jobs %v", tfJob.Name, err)
	// }

	if !(len(fakePodControl.Templates) == 2 && fakePodControl.Templates[0].Name == "test-tfjob-worker-1" && fakePodControl.Templates[1].Name == "test-tfjob-worker-2") {
		t.Error("Scale up workers test failed")
	}

	close(stopCh)
}

func TestIsWorker0Completed(t *testing.T) {
	newInt32 := func(in int32) *int32 {
		return &in
	}
	tests := []struct {
		// worker failed, succeeded, running num
		workers     [3]int32
		tfJob       *tfv1.TFJob
		replicas    map[commonv1.ReplicaType]*commonv1.ReplicaSpec
		expected    bool
		expectedErr bool
	}{
		{
			workers:     [3]int32{0, 0, 1},
			tfJob:       testutil.NewTFJobV2(1, 1, 0, 0, 0),
			expected:    false,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeWorker: {
					Replicas: newInt32(1),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
				tfv1.TFReplicaTypePS: {
					Replicas: newInt32(1),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
		{
			workers:     [3]int32{0, 1, 0},
			tfJob:       testutil.NewTFJobV2(1, 0, 0, 0, 0),
			expected:    true,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeWorker: {
					Replicas: newInt32(1),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
		{
			workers:     [3]int32{0, 0, 0},
			tfJob:       testutil.NewTFJobV2(0, 0, 1, 0, 0),
			expected:    true,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeMaster: {
					Replicas: newInt32(1),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
		{
			workers:     [3]int32{0, 0, 0},
			tfJob:       testutil.NewTFJobV2(0, 0, 0, 1, 0),
			expected:    true,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeChief: {
					Replicas: newInt32(1),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
		{
			workers:     [3]int32{1, 1, 0},
			tfJob:       testutil.NewTFJobV2(2, 0, 0, 0, 0),
			expected:    true,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeWorker: {
					Replicas: newInt32(2),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
		{
			workers:     [3]int32{1, 0, 1},
			tfJob:       testutil.NewTFJobV2(2, 0, 0, 0, 0),
			expected:    false,
			expectedErr: false,
			replicas: map[commonv1.ReplicaType]*commonv1.ReplicaSpec{
				tfv1.TFReplicaTypeWorker: {
					Replicas: newInt32(2),
					Template: testutil.NewTFReplicaSpecTemplate(),
				},
			},
		},
	}
	for _, tt := range tests {
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
		ctr, kubeInformerFactory, _ := newTFController(config, kubeClientSet,
			volcanoClientSet, tfJobClientSet, 0, options.ServerOption{})
		ctr.tfJobInformerSynced = testutil.AlwaysReady
		ctr.PodInformerSynced = testutil.AlwaysReady
		ctr.ServiceInformerSynced = testutil.AlwaysReady
		podIndexer := kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()

		// only related to worker status
		initializeReplicaStatuses(&tt.tfJob.Status, tfv1.TFReplicaTypeWorker)
		// set status and add pod to indexer
		setStatusForTest(tt.tfJob, tfv1.TFReplicaTypeWorker, tt.workers[0], tt.workers[1], tt.workers[2], false, true, podIndexer, t)

		got, err := ctr.IsWorker0Completed(tt.tfJob, tt.replicas)
		if (err != nil) != tt.expectedErr {
			t.Errorf("IsWorker0Completed() error = %v, wantErr %v", err, tt.expectedErr)
			return
		}
		if got != tt.expected {
			t.Errorf("IsWorker0Completed() got = %v, want %v", got, tt.expected)
		}
	}
}
