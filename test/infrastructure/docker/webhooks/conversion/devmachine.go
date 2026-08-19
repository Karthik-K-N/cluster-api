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

// DevMachine is a HubSpokeConverter for the DevMachine API type.
var DevMachine = ctrlconversion.NewHubSpokeConverter(&infrav1.DevMachine{},
	ctrlconversion.NewSpokeConverter(&infrav1beta1.DevMachine{}, ConvertDevMachineHubToV1Beta1, ConvertDevMachineV1Beta1ToHub),
)

// ConvertDevMachineV1Beta1ToHub converts a v1beta1 DevMachine to a hub DevMachine.
func ConvertDevMachineV1Beta1ToHub(_ context.Context, src *infrav1beta1.DevMachine, dst *infrav1.DevMachine) error {
	if err := infrav1beta1.Convert_v1beta1_DevMachine_To_v1beta2_DevMachine(src, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1.DevMachine{}
	ok, err := conversionutil.UnmarshalData(src, restored)
	if err != nil {
		return err
	}

	// Recover intent for bool values converted to *bool.
	initialization := infrav1.DevMachineInitializationStatus{}
	restoredDevMachineProvisioned := restored.Status.Initialization.Provisioned
	clusterv1.Convert_bool_To_Pointer_bool(src.Status.Ready, ok, restoredDevMachineProvisioned, &initialization.Provisioned)
	if !reflect.DeepEqual(initialization, infrav1.DevMachineInitializationStatus{}) {
		dst.Status.Initialization = initialization
	}

	if ok {
		dst.Status.FailureDomain = restored.Status.FailureDomain
	}
	return nil
}

// ConvertDevMachineHubToV1Beta1 converts a hub DevMachine to a v1beta1 DevMachine.
func ConvertDevMachineHubToV1Beta1(_ context.Context, src *infrav1.DevMachine, dst *infrav1beta1.DevMachine) error {
	if err := infrav1beta1.Convert_v1beta2_DevMachine_To_v1beta1_DevMachine(src, dst, nil); err != nil {
		return err
	}

	if dst.Spec.ProviderID != nil && *dst.Spec.ProviderID == "" {
		dst.Spec.ProviderID = nil
	}

	return conversionutil.MarshalDataUnsafeNoCopy(src, dst)
}

// DevMachineTemplate is a HubSpokeConverter for the DevMachineTemplate API type.
var DevMachineTemplate = ctrlconversion.NewHubSpokeConverter(&infrav1.DevMachineTemplate{},
	ctrlconversion.NewSpokeConverter(&infrav1beta1.DevMachineTemplate{}, ConvertDevMachineTemplateHubToV1Beta1, ConvertDevMachineTemplateV1Beta1ToHub),
)

// ConvertDevMachineTemplateV1Beta1ToHub converts a v1beta1 DevMachineTemplate to a hub DevMachineTemplate.
func ConvertDevMachineTemplateV1Beta1ToHub(_ context.Context, src *infrav1beta1.DevMachineTemplate, dst *infrav1.DevMachineTemplate) error {
	if err := infrav1beta1.Convert_v1beta1_DevMachineTemplate_To_v1beta2_DevMachineTemplate(src, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1.DevMachineTemplate{}
	ok, err := conversionutil.UnmarshalData(src, restored)
	if err != nil {
		return err
	}

	if ok {
		dst.Status = restored.Status
	}

	return nil
}

// ConvertDevMachineTemplateHubToV1Beta1 converts a hub DevMachineTemplate to a v1beta1 DevMachineTemplate.
func ConvertDevMachineTemplateHubToV1Beta1(_ context.Context, src *infrav1.DevMachineTemplate, dst *infrav1beta1.DevMachineTemplate) error {
	if err := infrav1beta1.Convert_v1beta2_DevMachineTemplate_To_v1beta1_DevMachineTemplate(src, dst, nil); err != nil {
		return err
	}

	if dst.Spec.Template.Spec.ProviderID != nil && *dst.Spec.Template.Spec.ProviderID == "" {
		dst.Spec.Template.Spec.ProviderID = nil
	}

	return conversionutil.MarshalDataUnsafeNoCopy(src, dst)
}
