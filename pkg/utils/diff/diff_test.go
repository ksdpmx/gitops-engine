package diff

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/yudai/gojsondiff"
	"github.com/yudai/gojsondiff/formatter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/argoproj/gitops-engine/pkg/utils/errors"
)

var (
	formatOpts = formatter.AsciiFormatterConfig{
		Coloring: false,
	}
)

func printDiff(result *DiffResult, left *unstructured.Unstructured) (string, error) {
	leftData, err := json.Marshal(left)
	if err != nil {
		return "", err
	}

	diff, err := gojsondiff.New().Compare(leftData, result.PredictedLive)
	if err != nil {
		return "", err
	}
	if !diff.Modified() {
		return "", nil
	}

	asciiFmt := formatter.NewAsciiFormatter(left.Object, formatOpts)
	return asciiFmt.Format(diff)
}

func toUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	uObj, err := runtime.NewTestUnstructuredConverter(equality.Semantic).ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: uObj}, nil
}

func mustToUnstructured(obj interface{}) *unstructured.Unstructured {
	un, err := toUnstructured(obj)
	errors.CheckError(err)
	return un
}

func unmarshalFile(path string) *unstructured.Unstructured {
	data, err := ioutil.ReadFile(path)
	errors.CheckError(err)
	var un unstructured.Unstructured
	err = json.Unmarshal(data, &un.Object)
	errors.CheckError(err)
	return &un
}

func newDeployment() *appsv1.Deployment {
	var two int32 = 2
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1beta1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &two,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "demo",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "demo",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "demo",
							Image: "gcr.io/kuar-demo/kuard-amd64:1",
							Ports: []v1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
}

func diff(t *testing.T, config, live *unstructured.Unstructured, options DiffOptions) *DiffResult {
	res, err := Diff(config, live, nil, options)
	assert.NoError(t, err)
	return res
}

func TestDiff(t *testing.T) {
	leftDep := newDeployment()
	leftUn := mustToUnstructured(leftDep)

	diffRes := diff(t, leftUn, leftUn, GetDefaultDiffOptions())
	assert.False(t, diffRes.Modified)
	ascii, err := printDiff(diffRes, leftUn)
	assert.Nil(t, err)
	if ascii != "" {
		log.Println(ascii)
	}
}

func TestDiffWithNils(t *testing.T) {
	dep := newDeployment()
	resource := mustToUnstructured(dep)

	diffRes := diff(t, nil, resource, GetDefaultDiffOptions())
	// NOTE: if live is non-nil, and config is nil, this is not considered difference
	// This "difference" is checked at the comparator.
	assert.False(t, diffRes.Modified)
	diffRes, err := TwoWayDiff(nil, resource)
	assert.NoError(t, err)
	assert.False(t, diffRes.Modified)

	diffRes = diff(t, resource, nil, GetDefaultDiffOptions())
	assert.True(t, diffRes.Modified)
	diffRes, err = TwoWayDiff(resource, nil)
	assert.NoError(t, err)
	assert.True(t, diffRes.Modified)
}

func TestDiffNilFieldInLive(t *testing.T) {
	leftDep := newDeployment()
	rightDep := leftDep.DeepCopy()

	leftUn := mustToUnstructured(leftDep)
	rightUn := mustToUnstructured(rightDep)
	err := unstructured.SetNestedField(rightUn.Object, nil, "spec")
	assert.Nil(t, err)

	diffRes := diff(t, leftUn, rightUn, GetDefaultDiffOptions())
	assert.True(t, diffRes.Modified)
}

func TestDiffArraySame(t *testing.T) {
	leftDep := newDeployment()
	rightDep := leftDep.DeepCopy()

	leftUn := mustToUnstructured(leftDep)
	rightUn := mustToUnstructured(rightDep)

	left := []*unstructured.Unstructured{leftUn}
	right := []*unstructured.Unstructured{rightUn}
	diffResList, err := DiffArray(left, right, nil, GetDefaultDiffOptions())
	assert.Nil(t, err)
	assert.False(t, diffResList.Modified)
}

func TestDiffArrayAdditions(t *testing.T) {
	leftDep := newDeployment()
	rightDep := leftDep.DeepCopy()
	rightDep.Status.Replicas = 1

	leftUn := mustToUnstructured(leftDep)
	rightUn := mustToUnstructured(rightDep)

	left := []*unstructured.Unstructured{leftUn}
	right := []*unstructured.Unstructured{rightUn}
	diffResList, err := DiffArray(left, right, nil, GetDefaultDiffOptions())
	assert.Nil(t, err)
	assert.False(t, diffResList.Modified)
}

func TestDiffArrayModification(t *testing.T) {
	leftDep := newDeployment()
	rightDep := leftDep.DeepCopy()
	ten := int32(10)
	rightDep.Spec.Replicas = &ten

	leftUn := mustToUnstructured(leftDep)
	rightUn := mustToUnstructured(rightDep)

	left := []*unstructured.Unstructured{leftUn}
	right := []*unstructured.Unstructured{rightUn}
	diffResList, err := DiffArray(left, right, nil, GetDefaultDiffOptions())
	assert.Nil(t, err)
	assert.True(t, diffResList.Modified)
}

// TestThreeWayDiff will perform a diff when there is a kubectl.kubernetes.io/last-applied-configuration
// present in the live object.
func TestThreeWayDiff(t *testing.T) {
	// 1. get config and live to be the same. Both have a foo annotation.
	configDep := newDeployment()
	configDep.ObjectMeta.Namespace = ""
	configDep.Annotations = map[string]string{
		"foo": "bar",
	}
	liveDep := configDep.DeepCopy()

	// 2. add a extra field to the live. this simulates kubernetes adding default values in the
	// object. We should not consider defaulted values as a difference
	liveDep.SetNamespace("default")
	configUn := mustToUnstructured(configDep)
	liveUn := mustToUnstructured(liveDep)
	res := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.False(t, res.Modified) {
		ascii, err := printDiff(res, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}

	// 3. Add a last-applied-configuration annotation in the live. There should still not be any
	// difference
	configBytes, err := json.Marshal(configDep)
	assert.Nil(t, err)
	liveDep.Annotations[v1.LastAppliedConfigAnnotation] = string(configBytes)
	configUn = mustToUnstructured(configDep)
	liveUn = mustToUnstructured(liveDep)
	res = diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.False(t, res.Modified) {
		ascii, err := printDiff(res, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}

	// 4. Remove the foo annotation from config and perform the diff again. We should detect a
	// difference since three-way diff detects the removal of a managed field
	delete(configDep.Annotations, "foo")
	configUn = mustToUnstructured(configDep)
	liveUn = mustToUnstructured(liveDep)
	res = diff(t, configUn, liveUn, GetDefaultDiffOptions())
	assert.True(t, res.Modified)

	// 5. Just to prove three way diff incorporates last-applied-configuration, remove the
	// last-applied-configuration annotation from the live object, and redo the diff. This time,
	// the diff will report not modified (because we have no way of knowing what was a defaulted
	// field without this annotation)
	delete(liveDep.Annotations, v1.LastAppliedConfigAnnotation)
	configUn = mustToUnstructured(configDep)
	liveUn = mustToUnstructured(liveDep)
	res = diff(t, configUn, liveUn, GetDefaultDiffOptions())
	ascii, err := printDiff(res, liveUn)
	assert.Nil(t, err)
	if ascii != "" {
		log.Println(ascii)
	}
	assert.False(t, res.Modified)
}

var demoConfig = `
{
  "apiVersion": "v1",
  "kind": "ServiceAccount",
  "metadata": {
    "labels": {
      "app.kubernetes.io/instance": "argocd-demo"
    },
    "name": "argocd-application-controller"
  }
}
`

var demoLive = `
{
  "apiVersion": "v1",
  "kind": "ServiceAccount",
  "metadata": {
    "annotations": {
      "kubectl.kubernetes.io/last-applied-configuration": "{\"apiVersion\":\"v1\",\"kind\":\"ServiceAccount\",\"metadata\":{\"annotations\":{},\"labels\":{\"app.kubernetes.io/instance\":\"argocd-demo\"},\"name\":\"argocd-application-controller\",\"namespace\":\"argocd-demo\"}}\n"
    },
    "creationTimestamp": "2018-04-16T22:08:57Z",
    "labels": {
      "app.kubernetes.io/instance": "argocd-demo"
    },
    "name": "argocd-application-controller",
    "namespace": "argocd-demo",
    "resourceVersion": "7584502",
    "selfLink": "/api/v1/namespaces/argocd-demo/serviceaccounts/argocd-application-controller",
    "uid": "c22bb2b4-41c2-11e8-978a-028445d52ec8"
  },
  "secrets": [
    {
      "name": "argocd-application-controller-token-kfxct"
    }
  ]
}
`

// Tests a real world example
func TestThreeWayDiffExample1(t *testing.T) {
	var configUn, liveUn unstructured.Unstructured
	// NOTE: it is intentional to unmarshal to Unstructured.Object instead of just Unstructured
	// since it catches a case when we comparison fails due to subtle differences in types
	// (e.g. float vs. int)
	err := json.Unmarshal([]byte(demoConfig), &configUn.Object)
	assert.Nil(t, err)
	err = json.Unmarshal([]byte(demoLive), &liveUn.Object)
	assert.Nil(t, err)
	dr := diff(t, &configUn, &liveUn, GetDefaultDiffOptions())
	assert.False(t, dr.Modified)
	ascii, err := printDiff(dr, &liveUn)
	assert.Nil(t, err)
	if ascii != "" {
		log.Println(ascii)
	}

}

// Test for ignoring aggregated cluster roles
func TestDiffOptionIgnoreAggregateRoles(t *testing.T) {
	// Test case 1: Ignore option is true, the rules in the role should be ignored
	{
		configUn := unmarshalFile("testdata/aggr-clusterrole-config.json")
		liveUn := unmarshalFile("testdata/aggr-clusterrole-live.json")
		dr := diff(t, configUn, liveUn, DiffOptions{IgnoreAggregatedRoles: true})
		assert.False(t, dr.Modified)
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
	// Test case 2: Ignore option is false, the aggregation should produce a diff
	{
		configUn := unmarshalFile("testdata/aggr-clusterrole-config.json")
		liveUn := unmarshalFile("testdata/aggr-clusterrole-live.json")
		dr := diff(t, configUn, liveUn, DiffOptions{IgnoreAggregatedRoles: false})
		assert.True(t, dr.Modified)
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

func TestThreeWayDiffExample2(t *testing.T) {
	configUn := unmarshalFile("testdata/elasticsearch-config.json")
	liveUn := unmarshalFile("testdata/elasticsearch-live.json")
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	assert.False(t, dr.Modified)
	ascii, err := printDiff(dr, liveUn)
	assert.Nil(t, err)
	log.Println(ascii)
}

// TestThreeWayDiffExample2WithDifference is same as TestThreeWayDiffExample2 but with differences
func TestThreeWayDiffExample2WithDifference(t *testing.T) {
	configUn := unmarshalFile("testdata/elasticsearch-config.json")
	liveUn := unmarshalFile("testdata/elasticsearch-live.json")
	labels := configUn.GetLabels()
	// add a new label
	labels["foo"] = "bar"
	// modify a label
	labels["chart"] = "elasticsearch-1.7.1"
	// remove an existing label
	delete(labels, "release")
	configUn.SetLabels(labels)

	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	assert.True(t, dr.Modified)
	ascii, err := printDiff(dr, liveUn)
	assert.Nil(t, err)
	log.Println(ascii)

	// Check that we indicate missing/extra/changed correctly
	showsMissing := 0
	showsExtra := 0
	showsChanged := 0
	for _, line := range strings.Split(ascii, "\n") {
		if strings.HasPrefix(line, `+      "foo": "bar"`) {
			showsMissing++
		}
		if strings.HasPrefix(line, `-      "release": "elasticsearch4"`) {
			showsExtra++
		}
		if strings.HasPrefix(line, `+      "chart": "elasticsearch-1.7.1"`) {
			showsChanged++
		}
		if strings.HasPrefix(line, `-      "chart": "elasticsearch-1.7.0"`) {
			showsChanged++
		}
	}
	assert.Equal(t, 1, showsMissing)
	assert.Equal(t, 1, showsExtra)
	assert.Equal(t, 2, showsChanged)
}

func TestThreeWayDiffExplicitNamespace(t *testing.T) {
	configUn := unmarshalFile("testdata/spinnaker-sa-config.json")
	liveUn := unmarshalFile("testdata/spinnaker-sa-live.json")
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	assert.False(t, dr.Modified)
	ascii, err := printDiff(dr, liveUn)
	assert.Nil(t, err)
	log.Println(ascii)
}

func TestRemoveNamespaceAnnotation(t *testing.T) {
	obj := removeNamespaceAnnotation(&unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "test",
			"namespace": "default",
		},
	}})
	assert.Equal(t, "", obj.GetNamespace())

	obj = removeNamespaceAnnotation(&unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        "test",
			"namespace":   "default",
			"annotations": make(map[string]interface{}),
		},
	}})
	assert.Equal(t, "", obj.GetNamespace())
	assert.Nil(t, obj.GetAnnotations())
}

const customObjConfig = `
apiVersion: foo.io/v1
kind: Foo
metadata:
  name: my-foo
  namespace: kube-system
spec:
  foo: bar
`

const customObjLive = `
apiVersion: foo.io/v1
kind: Foo
metadata:
  creationTimestamp: 2018-07-17 09:17:05 UTC
  name: my-foo
  resourceVersion: '10308211'
  selfLink: "/apis/rbac.authorization.k8s.io/v1/clusterroles/argocd-manager-role"
  uid: 2c3d5405-89a2-11e8-aff0-42010a8a0fc6
spec:
  foo: bar
`

func TestIgnoreNamespaceForClusterScopedResources(t *testing.T) {
	var configUn unstructured.Unstructured
	var liveUn unstructured.Unstructured
	err := yaml.Unmarshal([]byte(customObjLive), &liveUn)
	assert.Nil(t, err)
	err = yaml.Unmarshal([]byte(customObjConfig), &configUn)
	assert.Nil(t, err)
	dr := diff(t, &configUn, &liveUn, GetDefaultDiffOptions())
	assert.False(t, dr.Modified)
}

const secretConfig = `
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
type: Opaque
stringData:
  foo: bar
  bar: "1234"
data:
  baz: cXV4
`

const secretLive = `
apiVersion: v1
kind: Secret
metadata:
  creationTimestamp: 2018-11-19T11:30:40Z
  name: my-secret
  namespace: argocd
  resourceVersion: "25848035"
  selfLink: /api/v1/namespaces/argocd/secrets/my-secret
  uid: 8b4a2766-ebee-11e8-93c0-42010a8a0013
type: Opaque
data:
  foo: YmFy
  bar: MTIzNA==
  baz: cXV4
`

func TestSecretStringData(t *testing.T) {
	var err error
	var configUn unstructured.Unstructured
	err = yaml.Unmarshal([]byte(secretConfig), &configUn)
	assert.Nil(t, err)

	var liveUn unstructured.Unstructured
	err = yaml.Unmarshal([]byte(secretLive), &liveUn)
	assert.Nil(t, err)

	dr := diff(t, &configUn, &liveUn, GetDefaultDiffOptions())
	if !assert.False(t, dr.Modified) {
		ascii, err := printDiff(dr, &liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

// This is invalid because foo is a number, not a string
const secretInvalidConfig = `
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
type: Opaque
stringData:
  foo: 1234
`

const secretInvalidLive = `
apiVersion: v1
kind: Secret
metadata:
  creationTimestamp: 2018-11-19T11:30:40Z
  name: my-secret
  namespace: argocd
  resourceVersion: "25848035"
  selfLink: /api/v1/namespaces/argocd/secrets/my-secret
  uid: 8b4a2766-ebee-11e8-93c0-42010a8a0013
type: Opaque
data:
  foo: MTIzNA==
`

func TestInvalidSecretStringData(t *testing.T) {
	var err error
	var configUn unstructured.Unstructured
	err = yaml.Unmarshal([]byte(secretInvalidConfig), &configUn)
	assert.Nil(t, err)

	var liveUn unstructured.Unstructured
	err = yaml.Unmarshal([]byte(secretInvalidLive), &liveUn)
	assert.Nil(t, err)

	dr := diff(t, &configUn, nil, GetDefaultDiffOptions())
	assert.True(t, dr.Modified)
}

func TestNullSecretData(t *testing.T) {
	configUn := unmarshalFile("testdata/wordpress-config.json")
	liveUn := unmarshalFile("testdata/wordpress-live.json")
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.False(t, dr.Modified) {
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

// TestRedactedSecretData tests we are able to perform diff on redacted secret data, which has
// invalid characters (*) for the the data byte array field.
func TestRedactedSecretData(t *testing.T) {
	configUn := unmarshalFile("testdata/wordpress-config.json")
	liveUn := unmarshalFile("testdata/wordpress-live.json")
	configData := configUn.Object["data"].(map[string]interface{})
	liveData := liveUn.Object["data"].(map[string]interface{})
	configData["wordpress-password"] = "***"
	configData["smtp-password"] = "***"
	liveData["wordpress-password"] = "******"
	liveData["smtp-password"] = "******"
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.True(t, dr.Modified) {
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

func TestNullRoleRule(t *testing.T) {
	configUn := unmarshalFile("testdata/grafana-clusterrole-config.json")
	liveUn := unmarshalFile("testdata/grafana-clusterrole-live.json")
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.False(t, dr.Modified) {
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

func TestNullCreationTimestamp(t *testing.T) {
	configUn := unmarshalFile("testdata/sealedsecret-config.json")
	liveUn := unmarshalFile("testdata/sealedsecret-live.json")
	dr := diff(t, configUn, liveUn, GetDefaultDiffOptions())
	if !assert.False(t, dr.Modified) {
		ascii, err := printDiff(dr, liveUn)
		assert.Nil(t, err)
		log.Println(ascii)
	}
}

func createSecret(data map[string]string) *unstructured.Unstructured {
	secret := corev1.Secret{TypeMeta: metav1.TypeMeta{Kind: "Secret"}}
	if data != nil {
		secret.Data = make(map[string][]byte)
		for k, v := range data {
			secret.Data[k] = []byte(v)
		}
	}

	return mustToUnstructured(&secret)
}

func secretData(obj *unstructured.Unstructured) map[string]interface{} {
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	return data
}

var (
	replacement1 = strings.Repeat("+", 8)
	replacement2 = strings.Repeat("+", 12)
	replacement3 = strings.Repeat("+", 16)
)

func TestHideSecretDataSameKeysDifferentValues(t *testing.T) {
	target, live, err := HideSecretData(
		createSecret(map[string]string{"key1": "test", "key2": "test"}),
		createSecret(map[string]string{"key1": "test-1", "key2": "test-1"}))
	assert.Nil(t, err)

	assert.Equal(t, map[string]interface{}{"key1": replacement1, "key2": replacement1}, secretData(target))
	assert.Equal(t, map[string]interface{}{"key1": replacement2, "key2": replacement2}, secretData(live))
}

func TestHideSecretDataSameKeysSameValues(t *testing.T) {
	target, live, err := HideSecretData(
		createSecret(map[string]string{"key1": "test", "key2": "test"}),
		createSecret(map[string]string{"key1": "test", "key2": "test"}))
	assert.Nil(t, err)

	assert.Equal(t, map[string]interface{}{"key1": replacement1, "key2": replacement1}, secretData(target))
	assert.Equal(t, map[string]interface{}{"key1": replacement1, "key2": replacement1}, secretData(live))
}

func TestHideSecretDataDifferentKeysDifferentValues(t *testing.T) {
	target, live, err := HideSecretData(
		createSecret(map[string]string{"key1": "test", "key2": "test"}),
		createSecret(map[string]string{"key2": "test-1", "key3": "test-1"}))
	assert.Nil(t, err)

	assert.Equal(t, map[string]interface{}{"key1": replacement1, "key2": replacement1}, secretData(target))
	assert.Equal(t, map[string]interface{}{"key2": replacement2, "key3": replacement1}, secretData(live))
}

func TestHideSecretDataLastAppliedConfig(t *testing.T) {
	lastAppliedSecret := createSecret(map[string]string{"key1": "test1"})
	targetSecret := createSecret(map[string]string{"key1": "test2"})
	liveSecret := createSecret(map[string]string{"key1": "test3"})
	lastAppliedStr, err := json.Marshal(lastAppliedSecret)
	assert.Nil(t, err)
	liveSecret.SetAnnotations(map[string]string{corev1.LastAppliedConfigAnnotation: string(lastAppliedStr)})

	target, live, err := HideSecretData(targetSecret, liveSecret)
	assert.Nil(t, err)
	err = json.Unmarshal([]byte(live.GetAnnotations()[corev1.LastAppliedConfigAnnotation]), &lastAppliedSecret)
	assert.Nil(t, err)

	assert.Equal(t, map[string]interface{}{"key1": replacement1}, secretData(target))
	assert.Equal(t, map[string]interface{}{"key1": replacement2}, secretData(live))
	assert.Equal(t, map[string]interface{}{"key1": replacement3}, secretData(lastAppliedSecret))

}

func TestRemarshal(t *testing.T) {
	manifest := []byte(`
apiVersion: v1
kind: ServiceAccount
imagePullSecrets: []
metadata:
  name: my-sa
`)
	var un unstructured.Unstructured
	err := yaml.Unmarshal(manifest, &un)
	assert.NoError(t, err)
	newUn := remarshal(&un)
	_, ok := newUn.Object["imagePullSecrets"]
	assert.False(t, ok)
	metadata := newUn.Object["metadata"].(map[string]interface{})
	_, ok = metadata["creationTimestamp"]
	assert.False(t, ok)
}

func TestRemarshalResources(t *testing.T) {
	manifest := []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
  - image: nginx:1.7.9
    name: nginx
    resources:
      requests:
        cpu: 0.2
`)
	un := unstructured.Unstructured{}
	err := yaml.Unmarshal(manifest, &un)
	assert.NoError(t, err)
	requestsBefore := un.Object["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})["resources"].(map[string]interface{})["requests"].(map[string]interface{})
	log.Println(requestsBefore)
	newUn := remarshal(&un)
	requestsAfter := newUn.Object["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})["resources"].(map[string]interface{})["requests"].(map[string]interface{})
	log.Println(requestsAfter)
	assert.Equal(t, float64(0.2), requestsBefore["cpu"])
	assert.Equal(t, "200m", requestsAfter["cpu"])
}