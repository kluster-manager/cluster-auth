/*
Copyright AppsCode Inc. and Contributors.

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

package manager

import (
	"embed"

	"github.com/kluster-manager/cluster-auth/pkg/common"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/yaml"
)

//go:embed all:agent-manifests
var FS embed.FS

func GetDefaultValues(registryFQDN string) addonfactory.GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
		data, err := FS.ReadFile("agent-manifests/cluster-auth/values.yaml")
		if err != nil {
			return nil, err
		}

		var values map[string]any
		if err = yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}

		if registryFQDN != "" {
			err = unstructured.SetNestedField(values, registryFQDN, "registryFQDN")
			if err != nil {
				return nil, err
			}
		}

		if err = unstructured.SetNestedField(values, common.HubKubeconfigSecretName, "hubKubeconfigSecretName"); err != nil {
			return nil, err
		}

		if err = unstructured.SetNestedField(values, "Always", "imagePullPolicy"); err != nil {
			return nil, err
		}

		if err = unstructured.SetNestedField(values, cluster.Name, "clusterName"); err != nil {
			return nil, err
		}

		return values, nil
	}
}
