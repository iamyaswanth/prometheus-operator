// Copyright 2020 The prometheus-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package thanos

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/kylelemons/godebug/pretty"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	defaultTestConfig = Config{
		ConfigReloaderImage:    "jimmidyson/configmap-reload:latest",
		ConfigReloaderCPU:      "100m",
		ConfigReloaderMemory:   "25Mi",
		ThanosDefaultBaseImage: "quay.io/thanos/thanos",
	}
	emptyQueryEndpoints = []string{""}
)

func TestStatefulSetLabelingAndAnnotations(t *testing.T) {
	labels := map[string]string{
		"testlabel": "testlabelvalue",
	}
	annotations := map[string]string{
		"testannotation": "testannotationvalue",
		"kubectl.kubernetes.io/last-applied-configuration": "something",
		"kubectl.kubernetes.io/something":                  "something",
	}
	// kubectl annotations must not be on the statefulset so kubectl does
	// not manage the generated object
	expectedAnnotations := map[string]string{
		"testannotation": "testannotationvalue",
	}

	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
		},
	}, nil, defaultTestConfig, nil)

	require.NoError(t, err)

	if !reflect.DeepEqual(labels, sset.Labels) {
		t.Log(pretty.Compare(labels, sset.Labels))
		t.Fatal("Labels are not properly being propagated to the StatefulSet")
	}

	if !reflect.DeepEqual(expectedAnnotations, sset.Annotations) {
		t.Log(pretty.Compare(expectedAnnotations, sset.Annotations))
		t.Fatal("Annotations are not properly being propagated to the StatefulSet")
	}
}

func TestPodLabelsAnnotations(t *testing.T) {
	annotations := map[string]string{
		"testannotation": "testvalue",
	}
	labels := map[string]string{
		"testlabel": "testvalue",
	}
	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
			PodMetadata: &monitoringv1.EmbeddedObjectMetadata{
				Annotations: annotations,
				Labels:      labels,
			},
		},
	}, nil, defaultTestConfig, nil)
	require.NoError(t, err)
	if _, ok := sset.Spec.Template.ObjectMeta.Labels["testlabel"]; !ok {
		t.Fatal("Pod labes are not properly propagated")
	}
	if !reflect.DeepEqual(annotations, sset.Spec.Template.ObjectMeta.Annotations) {
		t.Fatal("Pod annotaitons are not properly propagated")
	}
}

func TestStatefulSetVolumes(t *testing.T) {
	expected := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "thanos-ruler-foo-data",
									ReadOnly:  false,
									MountPath: "/thanos/data",
									SubPath:   "",
								},
								{
									Name:      "rules-configmap-one",
									ReadOnly:  false,
									MountPath: "/etc/thanos/rules/rules-configmap-one",
									SubPath:   "",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "rules-configmap-one",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "rules-configmap-one",
									},
								},
							},
						},
						{
							Name: "thanos-ruler-foo-data",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{
									Medium: "",
								},
							},
						},
					},
				},
			},
		},
	}
	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
		},
	}, nil, defaultTestConfig, []string{"rules-configmap-one"})
	require.NoError(t, err)
	if !reflect.DeepEqual(expected.Spec.Template.Spec.Volumes, sset.Spec.Template.Spec.Volumes) {
		fmt.Println(pretty.Compare(expected.Spec.Template.Spec.Volumes, sset.Spec.Template.Spec.Volumes))
		t.Fatal("expected volumes to match")
	}

	if !reflect.DeepEqual(expected.Spec.Template.Spec.Containers[0].VolumeMounts, sset.Spec.Template.Spec.Containers[0].VolumeMounts) {
		fmt.Println(pretty.Compare(expected.Spec.Template.Spec.Containers[0].VolumeMounts, sset.Spec.Template.Spec.Containers[0].VolumeMounts))
		t.Fatal("expected volume mounts to match")
	}
}

func TestTracing(t *testing.T) {
	testKey := "thanos-tracing-config-secret"

	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
			TracingConfig: &v1.SecretKeySelector{
				Key: testKey,
			},
		},
	}, nil, defaultTestConfig, nil)
	if err != nil {
		t.Fatalf("Unexpected error while making StatefulSet: %v", err)
	}

	if sset.Spec.Template.Spec.Containers[0].Name != "thanos-ruler" {
		t.Fatalf("expected 1st containers to be thanos-ruler, got %s", sset.Spec.Template.Spec.Containers[0].Name)
	}

	var containsEnvVar bool
	for _, env := range sset.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TRACING_CONFIG" {
			if env.ValueFrom.SecretKeyRef.Key == testKey {
				containsEnvVar = true
				break
			}
		}
	}
	if !containsEnvVar {
		t.Fatalf("Thanos ruler is missing expected TRACING_CONFIG env var with correct value")
	}

	{
		var containsArg bool
		const expectedArg = "--tracing.config=$(TRACING_CONFIG)"
		for _, arg := range sset.Spec.Template.Spec.Containers[0].Args {
			if arg == expectedArg {
				containsArg = true
				break
			}
		}
		if !containsArg {
			t.Fatalf("Thanos ruler is missing expected argument: %s", expectedArg)
		}
	}
}

func TestObjectStorage(t *testing.T) {
	testKey := "thanos-objstore-config-secret"

	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
			ObjectStorageConfig: &v1.SecretKeySelector{
				Key: testKey,
			},
		},
	}, nil, defaultTestConfig, nil)
	if err != nil {
		t.Fatalf("Unexpected error while making StatefulSet: %v", err)
	}

	if sset.Spec.Template.Spec.Containers[0].Name != "thanos-ruler" {
		t.Fatalf("expected 1st containers to be thanos-ruler, got %s", sset.Spec.Template.Spec.Containers[0].Name)
	}

	var containsEnvVar bool
	for _, env := range sset.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "OBJSTORE_CONFIG" {
			if env.ValueFrom.SecretKeyRef.Key == testKey {
				containsEnvVar = true
				break
			}
		}
	}
	if !containsEnvVar {
		t.Fatalf("Thanos ruler is missing expected OBJSTORE_CONFIG env var with correct value")
	}

	{
		var containsArg bool
		const expectedArg = "--objstore.config=$(OBJSTORE_CONFIG)"
		for _, arg := range sset.Spec.Template.Spec.Containers[0].Args {
			if arg == expectedArg {
				containsArg = true
				break
			}
		}
		if !containsArg {
			t.Fatalf("Thanos ruler is missing expected argument: %s", expectedArg)
		}
	}
}

func TestLabelsAndAlertDropLabels(t *testing.T) {
	labelPrefix := "--label="
	alertDropLabelPrefix := "--alert.label-drop="

	tests := []struct {
		Labels                  map[string]string
		AlertDropLabels         []string
		ExpectedLabels          []string
		ExpectedAlertDropLabels []string
	}{
		{
			Labels:                  nil,
			AlertDropLabels:         nil,
			ExpectedLabels:          []string{`thanos_ruler_replica="$(POD_NAME)"`},
			ExpectedAlertDropLabels: []string{"thanos_ruler_replica"},
		},
		{
			Labels:                  nil,
			AlertDropLabels:         []string{"test"},
			ExpectedLabels:          []string{`thanos_ruler_replica="$(POD_NAME)"`},
			ExpectedAlertDropLabels: []string{"thanos_ruler_replica", "test"},
		},
		{
			Labels: map[string]string{
				"test": "test",
			},
			AlertDropLabels:         nil,
			ExpectedLabels:          []string{`test="test"`},
			ExpectedAlertDropLabels: []string{},
		},
		{
			Labels: map[string]string{
				"test": "test",
			},
			AlertDropLabels:         []string{"test"},
			ExpectedLabels:          []string{`test="test"`},
			ExpectedAlertDropLabels: []string{"test"},
		},
		{
			Labels: map[string]string{
				"thanos_ruler_replica": "$(POD_NAME)",
				"test":                 "test",
			},
			AlertDropLabels:         []string{"test", "aaa"},
			ExpectedLabels:          []string{`test="test"`, `thanos_ruler_replica="$(POD_NAME)"`},
			ExpectedAlertDropLabels: []string{"test", "aaa"},
		},
	}
	for _, tc := range tests {
		actualLabels := []string{}
		actualDropLabels := []string{}
		sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: monitoringv1.ThanosRulerSpec{
				QueryEndpoints:  []string{""},
				Labels:          tc.Labels,
				AlertDropLabels: tc.AlertDropLabels,
			},
		}, nil, defaultTestConfig, nil)
		if err != nil {
			t.Fatalf("Unexpected error while making StatefulSet: %v", err)
		}

		ruler := sset.Spec.Template.Spec.Containers[0]
		if ruler.Name != "thanos-ruler" {
			t.Fatalf("Expected 1st containers to be thanos-ruler, got %s", ruler.Name)
		}

		for _, arg := range ruler.Args {
			if strings.HasPrefix(arg, labelPrefix) {
				actualLabels = append(actualLabels, strings.TrimPrefix(arg, labelPrefix))
			} else if strings.HasPrefix(arg, alertDropLabelPrefix) {
				actualDropLabels = append(actualDropLabels, strings.TrimPrefix(arg, alertDropLabelPrefix))
			}
		}
		sort.Slice(actualLabels, func(i, j int) bool {
			return actualLabels[i] < actualLabels[j]
		})
		if !reflect.DeepEqual(actualLabels, tc.ExpectedLabels) {
			t.Fatal("label sets mismatch")
		}

		if !reflect.DeepEqual(actualDropLabels, tc.ExpectedAlertDropLabels) {
			t.Fatal("alert drop label sets mismatch")
		}
	}
}

func TestAdditionalContainers(t *testing.T) {
	// The base to compare everything against
	baseSet, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		Spec: monitoringv1.ThanosRulerSpec{QueryEndpoints: emptyQueryEndpoints},
	}, nil, defaultTestConfig, nil)
	require.NoError(t, err)

	// Add an extra container
	addSset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
			Containers: []v1.Container{
				{
					Name: "extra-container",
				},
			},
		},
	}, nil, defaultTestConfig, nil)
	require.NoError(t, err)

	if len(baseSet.Spec.Template.Spec.Containers)+1 != len(addSset.Spec.Template.Spec.Containers) {
		t.Fatalf("container count mismatch")
	}

	// Adding a new container with the same name results in a merge and just one container
	const existingContainerName = "thanos-ruler"
	const containerImage = "madeUpContainerImage"
	modSset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints: emptyQueryEndpoints,
			Containers: []v1.Container{
				{
					Name:  existingContainerName,
					Image: containerImage,
				},
			},
		},
	}, nil, defaultTestConfig, nil)
	require.NoError(t, err)

	if len(baseSet.Spec.Template.Spec.Containers) != len(modSset.Spec.Template.Spec.Containers) {
		t.Fatalf("container count mismatch. container %s was added instead of merged", existingContainerName)
	}

	// Check that adding a container with an existing name results in a single patched container.
	for _, c := range modSset.Spec.Template.Spec.Containers {
		if c.Name == existingContainerName && c.Image != containerImage {
			t.Fatalf("expected container %s to have the image %s but got %s", existingContainerName, containerImage, c.Image)
		}
	}
}

func TestRetention(t *testing.T) {
	tests := []struct {
		specRetention     string
		expectedRetention string
	}{
		{"", "24h"},
		{"1d", "1d"},
	}

	for _, test := range tests {
		sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
			Spec: monitoringv1.ThanosRulerSpec{
				Retention:      test.specRetention,
				QueryEndpoints: emptyQueryEndpoints,
			},
		}, nil, defaultTestConfig, nil)
		if err != nil {
			t.Fatal(err)
		}

		trArgs := sset.Spec.Template.Spec.Containers[0].Args
		expectedRetentionArg := fmt.Sprintf("--tsdb.retention=%s", test.expectedRetention)
		found := false
		for _, flag := range trArgs {
			if flag == expectedRetentionArg {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("expected ThanosRuler args to contain %v, but got %v", expectedRetentionArg, trArgs)
		}
	}
}

func TestPodTemplateConfig(t *testing.T) {

	nodeSelector := map[string]string{
		"foo": "bar",
	}
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{},
		PodAffinity: &v1.PodAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
				v1.WeightedPodAffinityTerm{
					PodAffinityTerm: v1.PodAffinityTerm{
						Namespaces: []string{"foo"},
					},
					Weight: 100,
				},
			},
		},
		PodAntiAffinity: &v1.PodAntiAffinity{},
	}

	tolerations := []v1.Toleration{
		v1.Toleration{
			Key: "key",
		},
	}
	userid := int64(1234)
	securityContext := v1.PodSecurityContext{
		RunAsUser: &userid,
	}
	priorityClassName := "foo"
	serviceAccountName := "thanos-ruler-sa"

	sset, err := makeStatefulSet(&monitoringv1.ThanosRuler{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: monitoringv1.ThanosRulerSpec{
			QueryEndpoints:     emptyQueryEndpoints,
			NodeSelector:       nodeSelector,
			Affinity:           &affinity,
			Tolerations:        tolerations,
			SecurityContext:    &securityContext,
			PriorityClassName:  priorityClassName,
			ServiceAccountName: serviceAccountName,
		},
	}, nil, defaultTestConfig, nil)
	if err != nil {
		t.Fatalf("Unexpected error while making StatefulSet: %v", err)
	}

	if !reflect.DeepEqual(sset.Spec.Template.Spec.NodeSelector, nodeSelector) {
		t.Fatalf("expected node selector to match, want %v, got %v", nodeSelector, sset.Spec.Template.Spec.NodeSelector)
	}
	if !reflect.DeepEqual(*sset.Spec.Template.Spec.Affinity, affinity) {
		t.Fatalf("expected affinity to match, want %v, got %v", affinity, *sset.Spec.Template.Spec.Affinity)
	}
	if !reflect.DeepEqual(sset.Spec.Template.Spec.Tolerations, tolerations) {
		t.Fatalf("expected tolerations to match, want %v, got %v", tolerations, sset.Spec.Template.Spec.Tolerations)
	}
	if !reflect.DeepEqual(*sset.Spec.Template.Spec.SecurityContext, securityContext) {
		t.Fatalf("expected security context  to match, want %v, got %v", securityContext, *sset.Spec.Template.Spec.SecurityContext)
	}
	if sset.Spec.Template.Spec.PriorityClassName != priorityClassName {
		t.Fatalf("expected priority class name to match, want %s, got %s", priorityClassName, sset.Spec.Template.Spec.PriorityClassName)
	}
	if sset.Spec.Template.Spec.ServiceAccountName != serviceAccountName {
		t.Fatalf("expected service account name to match, want %s, got %s", serviceAccountName, sset.Spec.Template.Spec.ServiceAccountName)
	}
}