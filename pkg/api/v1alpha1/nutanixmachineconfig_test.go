package v1alpha1

import (
	"reflect"
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestGetNutanixMachineConfigsInvalidConfig(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		expectedErr string
	}{
		{
			name:        "non-existent-file",
			fileName:    "testdata/nutanix/non-existent-file.yaml",
			expectedErr: "open testdata/nutanix/non-existent-file.yaml: no such file or directory",
		},
		{
			name:        "invalid-file",
			fileName:    "testdata/invalid_format.yaml",
			expectedErr: "unable to find kind NutanixMachineConfig in file",
		},
		{
			name:        "invalid-cluster-extraneuous-field",
			fileName:    "testdata/nutanix/invalid-cluster.yaml",
			expectedErr: "unknown field \"idont\"",
		},
		{
			name:        "invalid kind",
			fileName:    "testdata/nutanix/invalid-kind.yaml",
			expectedErr: "unable to find kind NutanixMachineConfig in file",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conf, err := GetNutanixMachineConfigs(test.fileName)
			assert.Error(t, err)
			assert.Nil(t, conf)
			assert.Contains(t, err.Error(), test.expectedErr, "expected error", test.expectedErr, "got error", err)
		})
	}
}

func TestGetNutanixMachineConfigsValidConfig(t *testing.T) {
	expectedMachineConfig := &NutanixMachineConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       NutanixMachineConfigKind,
			APIVersion: SchemeBuilder.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "eksa-unit-test",
			Annotations: map[string]string{},
			Namespace:   defaultEksaNamespace,
		},
		Spec: NutanixMachineConfigSpec{
			SystemDiskSize: resource.MustParse("40Gi"),
			MemorySize:     resource.MustParse("8Gi"),
			VCPUSockets:    4,
			VCPUsPerSocket: 1,
			OSFamily:       Ubuntu,
			Image: NutanixResourceIdentifier{
				Type: NutanixIdentifierName,
				Name: ptr.String("prism-image"),
			},
			Cluster: NutanixResourceIdentifier{
				Type: NutanixIdentifierName,
				Name: ptr.String("prism-element"),
			},
			Subnet: NutanixResourceIdentifier{
				Type: NutanixIdentifierName,
				Name: ptr.String("prism-subnet"),
			},
			Users: []UserConfiguration{{
				Name:              "mySshUsername",
				SshAuthorizedKeys: []string{"mySshAuthorizedKey"},
			}},
		},
	}
	const machineConfName = "eksa-unit-test"

	tests := []struct {
		name        string
		fileName    string
		machineConf map[string]*NutanixMachineConfig
		assertions  func(t *testing.T, machineConf *NutanixMachineConfig)
	}{
		{
			name:     "valid-cluster",
			fileName: "testdata/nutanix/valid-cluster.yaml",
			machineConf: map[string]*NutanixMachineConfig{
				machineConfName: expectedMachineConfig,
			},
			assertions: func(t *testing.T, machineConf *NutanixMachineConfig) {},
		},
		{
			name:     "valid-cluster-extra-delimiter",
			fileName: "testdata/nutanix/valid-cluster-extra-delimiter.yaml",
			machineConf: map[string]*NutanixMachineConfig{
				machineConfName: expectedMachineConfig,
			},
			assertions: func(t *testing.T, machineConf *NutanixMachineConfig) {},
		},
		{
			name:     "valid-cluster-setters-getters",
			fileName: "testdata/nutanix/valid-cluster.yaml",
			machineConf: map[string]*NutanixMachineConfig{
				machineConfName: expectedMachineConfig,
			},
			assertions: func(t *testing.T, machineConf *NutanixMachineConfig) {
				assert.False(t, machineConf.IsReconcilePaused())
				machineConf.PauseReconcile()
				assert.True(t, machineConf.IsReconcilePaused())

				assert.False(t, machineConf.IsEtcd())
				machineConf.SetEtcd()
				assert.True(t, machineConf.IsEtcd())

				assert.False(t, machineConf.IsManaged())
				machineConf.SetManagedBy(machineConfName)
				assert.True(t, machineConf.IsManaged())

				assert.False(t, machineConf.IsControlPlane())
				machineConf.SetControlPlane()
				assert.True(t, machineConf.IsControlPlane())

				assert.Equal(t, Ubuntu, machineConf.OSFamily())
				assert.Equal(t, defaultEksaNamespace, machineConf.GetNamespace())
				assert.Equal(t, machineConfName, machineConf.GetName())
			},
		},
		{
			name:     "valid-cluster-marshal",
			fileName: "testdata/nutanix/valid-cluster.yaml",
			machineConf: map[string]*NutanixMachineConfig{
				machineConfName: expectedMachineConfig,
			},
			assertions: func(t *testing.T, machineConf *NutanixMachineConfig) {
				m := machineConf.Marshallable()
				require.NotNil(t, m)
				y, err := yaml.Marshal(m)
				assert.NoError(t, err)
				assert.NotNil(t, y)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conf, err := GetNutanixMachineConfigs(test.fileName)
			assert.NoError(t, err)
			require.NotNil(t, conf)
			assert.True(t, reflect.DeepEqual(test.machineConf, conf))
			test.assertions(t, conf[machineConfName])
		})
	}
}

func TestNewNutanixMachineConfigGenerate(t *testing.T) {
	machineConf := NewNutanixMachineConfigGenerate("eksa-unit-test", func(config *NutanixMachineConfigGenerate) {
		config.Spec.MemorySize = resource.MustParse("16Gi")
	})
	require.NotNil(t, machineConf)
	assert.Equal(t, "eksa-unit-test", machineConf.Name())
	assert.Equal(t, NutanixMachineConfigKind, machineConf.Kind())
	assert.Equal(t, SchemeBuilder.GroupVersion.String(), machineConf.APIVersion())
	assert.Equal(t, resource.MustParse("16Gi"), machineConf.Spec.MemorySize)
}
