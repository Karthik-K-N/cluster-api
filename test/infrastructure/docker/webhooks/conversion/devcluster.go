/*
Copyright 2026 The Kubernetes Authors.

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

package conversion

import (
	"context"
	"reflect"

	ctrlconversion "sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	infrav1beta1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1beta1"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1beta2"
	conversionutil "sigs.k8s.io/cluster-api/util/conversion"
)

// DevCluster is a HubSpokeConverter for the DevCluster API type.
var DevCluster = ctrlconversion.NewHubSpokeConverter(&infrav1.DevCluster{},
	ctrlconversion.NewSpokeConverter(&infrav1beta1.DevCluster{}, ConvertDevClusterHubToV1Beta1, ConvertDevClusterV1Beta1ToHub),
)

// ConvertDevClusterV1Beta1ToHub converts a v1beta1 DevCluster to a hub DevCluster.
func ConvertDevClusterV1Beta1ToHub(_ context.Context, src *infrav1beta1.DevCluster, dst *infrav1.DevCluster) error {
	if err := infrav1beta1.Convert_v1beta1_DevCluster_To_v1beta2_DevCluster(src, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1.DevCluster{}
	ok, err := conversionutil.UnmarshalData(src, restored)
	if err != nil {
		return err
	}

	// Recover intent for bool values converted to *bool.
	initialization := infrav1.DevClusterInitializationStatus{}
	restoredDevClusterProvisioned := restored.Status.Initialization.Provisioned
	clusterv1.Convert_bool_To_Pointer_bool(src.Status.Ready, ok, restoredDevClusterProvisioned, &initialization.Provisioned)
	if !reflect.DeepEqual(initialization, infrav1.DevClusterInitializationStatus{}) {
		dst.Status.Initialization = initialization
	}

	return nil
}

// ConvertDevClusterHubToV1Beta1 converts a hub DevCluster to a v1beta1 DevCluster.
func ConvertDevClusterHubToV1Beta1(_ context.Context, src *infrav1.DevCluster, dst *infrav1beta1.DevCluster) error {
	if err := infrav1beta1.Convert_v1beta2_DevCluster_To_v1beta1_DevCluster(src, dst, nil); err != nil {
		return err
	}

	return conversionutil.MarshalDataUnsafeNoCopy(src, dst)
}

// DevClusterTemplate is a HubSpokeConverter for the DevClusterTemplate API type.
var DevClusterTemplate = ctrlconversion.NewHubSpokeConverter(&infrav1.DevClusterTemplate{},
	ctrlconversion.NewSpokeConverter(&infrav1beta1.DevClusterTemplate{}, ConvertDevClusterTemplateHubToV1Beta1, ConvertDevClusterTemplateV1Beta1ToHub),
)

// ConvertDevClusterTemplateV1Beta1ToHub converts a v1beta1 DevClusterTemplate to a hub DevClusterTemplate.
func ConvertDevClusterTemplateV1Beta1ToHub(_ context.Context, src *infrav1beta1.DevClusterTemplate, dst *infrav1.DevClusterTemplate) error {
	return infrav1beta1.Convert_v1beta1_DevClusterTemplate_To_v1beta2_DevClusterTemplate(src, dst, nil)
}

// ConvertDevClusterTemplateHubToV1Beta1 converts a hub DevClusterTemplate to a v1beta1 DevClusterTemplate.
func ConvertDevClusterTemplateHubToV1Beta1(_ context.Context, src *infrav1.DevClusterTemplate, dst *infrav1beta1.DevClusterTemplate) error {
	return infrav1beta1.Convert_v1beta2_DevClusterTemplate_To_v1beta1_DevClusterTemplate(src, dst, nil)
}
