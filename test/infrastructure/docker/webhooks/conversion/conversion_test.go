//go:build !race

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
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/randfill"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	infrav1beta1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1beta1"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1beta2"
	conversionutil "sigs.k8s.io/cluster-api/util/conversion"
)

// Test is disabled when the race detector is enabled (via "//go:build !race" above) because otherwise the fuzz tests would just time out.

func TestFuzzyConversion(t *testing.T) {
	t.Run("for DevCluster", conversionutil.SpokeConverterFuzzTestFunc(
		conversionutil.SpokeConverterFuzzTestFuncInput[*infrav1.DevCluster, *infrav1beta1.DevCluster]{
			ConvertSpokeToHubFunc: ConvertDevClusterV1Beta1ToHub,
			ConvertHubToSpokeFunc: ConvertDevClusterHubToV1Beta1,
			FuzzerFuncs:           []fuzzer.FuzzerFuncs{DevClusterFuzzFunc},
		}),
	)

	t.Run("for DevClusterTemplate", conversionutil.SpokeConverterFuzzTestFunc(
		conversionutil.SpokeConverterFuzzTestFuncInput[*infrav1.DevClusterTemplate, *infrav1beta1.DevClusterTemplate]{
			ConvertSpokeToHubFunc: ConvertDevClusterTemplateV1Beta1ToHub,
			ConvertHubToSpokeFunc: ConvertDevClusterTemplateHubToV1Beta1,
			FuzzerFuncs:           []fuzzer.FuzzerFuncs{DevClusterTemplateFuzzFunc},
		}),
	)

	t.Run("for DevMachine", conversionutil.SpokeConverterFuzzTestFunc(
		conversionutil.SpokeConverterFuzzTestFuncInput[*infrav1.DevMachine, *infrav1beta1.DevMachine]{
			ConvertSpokeToHubFunc: ConvertDevMachineV1Beta1ToHub,
			ConvertHubToSpokeFunc: ConvertDevMachineHubToV1Beta1,
			FuzzerFuncs:           []fuzzer.FuzzerFuncs{DevMachineFuzzFunc},
		}),
	)

	t.Run("for DevMachineTemplate", conversionutil.SpokeConverterFuzzTestFunc(
		conversionutil.SpokeConverterFuzzTestFuncInput[*infrav1.DevMachineTemplate, *infrav1beta1.DevMachineTemplate]{
			ConvertSpokeToHubFunc: ConvertDevMachineTemplateV1Beta1ToHub,
			ConvertHubToSpokeFunc: ConvertDevMachineTemplateHubToV1Beta1,
			FuzzerFuncs:           []fuzzer.FuzzerFuncs{DevMachineTemplateFuzzFunc},
		}),
	)
}

func hubFailureDomain(in *clusterv1.FailureDomain, c randfill.Continue) {
	c.FillNoCustom(in)

	if in.ControlPlane == nil {
		in.ControlPlane = ptr.To(false)
	}
}

// DevClusterFuzzFunc returns fuzzer funcs for DevCluster conversion.
func DevClusterFuzzFunc(_ runtimeserializer.CodecFactory) []any {
	return []any{
		hubDevClusterStatus,
		hubFailureDomain,
		spokeDevClusterStatus,
	}
}

func hubDevClusterStatus(in *infrav1.DevClusterStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	if in.Deprecated != nil {
		if in.Deprecated.V1Beta1 == nil || reflect.DeepEqual(in.Deprecated.V1Beta1, &infrav1.DevClusterV1Beta1DeprecatedStatus{}) {
			in.Deprecated = nil
		}
	}
}

func spokeDevClusterStatus(in *infrav1beta1.DevClusterStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	// Drop empty structs with only omit empty fields.
	if in.V1Beta2 != nil {
		if reflect.DeepEqual(in.V1Beta2, &infrav1beta1.DevClusterV1Beta2Status{}) {
			in.V1Beta2 = nil
		}
	}
}

// DevClusterTemplateFuzzFunc returns fuzzer funcs for DevClusterTemplate conversion.
func DevClusterTemplateFuzzFunc(_ runtimeserializer.CodecFactory) []any {
	return []any{
		hubFailureDomain,
	}
}

// DevMachineFuzzFunc returns fuzzer funcs for DevMachine conversion.
func DevMachineFuzzFunc(_ runtimeserializer.CodecFactory) []any {
	return []any{
		hubDevMachineStatus,
		spokeDevMachineSpec,
		spokeDevMachineStatus,
	}
}

func hubDevMachineStatus(in *infrav1.DevMachineStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	if in.Deprecated != nil {
		if in.Deprecated.V1Beta1 == nil || reflect.DeepEqual(in.Deprecated.V1Beta1, &infrav1.DevMachineV1Beta1DeprecatedStatus{}) {
			in.Deprecated = nil
		}
	}
}

func spokeDevMachineSpec(in *infrav1beta1.DevMachineSpec, c randfill.Continue) {
	c.FillNoCustom(in)

	if in.ProviderID != nil && *in.ProviderID == "" {
		in.ProviderID = nil
	}
}

func spokeDevMachineStatus(in *infrav1beta1.DevMachineStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	// Drop empty structs with only omit empty fields.
	if in.V1Beta2 != nil {
		if reflect.DeepEqual(in.V1Beta2, &infrav1beta1.DevMachineV1Beta2Status{}) {
			in.V1Beta2 = nil
		}
	}
}

// DevMachineTemplateFuzzFunc returns fuzzer funcs for DevMachineTemplate conversion.
func DevMachineTemplateFuzzFunc(_ runtimeserializer.CodecFactory) []any {
	return []any{
		spokeDevMachineSpec,
	}
}
