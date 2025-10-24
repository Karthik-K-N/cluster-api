/*
Copyright 2025 The Kubernetes Authors.

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

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/internal/util/compare"
	patchutil "sigs.k8s.io/cluster-api/internal/util/patch"
	"sigs.k8s.io/cluster-api/internal/util/ssa"
)

func (r *KubeadmControlPlaneReconciler) canUpdateMachine(ctx context.Context, machine *clusterv1.Machine, machineUpToDateResult internal.UpToDateResult) (bool, error) {
	if r.overrideCanUpdateMachineFunc != nil {
		return r.overrideCanUpdateMachineFunc(ctx, machine, machineUpToDateResult)
	}

	log := ctrl.LoggerFrom(ctx)

	// Machine cannot be updated in-place if the feature gate is not enabled.
	if !feature.Gates.Enabled(feature.InPlaceUpdates) {
		return false, nil
	}

	// Machine cannot be updated in-place if the UpToDate func was not able to provide all objects,
	// e.g. if the InfraMachine or KubeadmConfig was deleted.
	if machineUpToDateResult.DesiredMachine == nil ||
		machineUpToDateResult.CurrentInfraMachine == nil ||
		machineUpToDateResult.DesiredInfraMachine == nil ||
		machineUpToDateResult.CurrentKubeadmConfig == nil ||
		machineUpToDateResult.DesiredKubeadmConfig == nil {
		return false, nil
	}

	extensionHandlers, err := r.RuntimeClient.GetAllExtensions(ctx, runtimehooksv1.CanUpdateMachine, machine)
	if err != nil {
		return false, err
	}
	// Machine cannot be updated in-place if no CanUpdateMachine extensions are registered.
	if len(extensionHandlers) == 0 {
		return false, nil
	}
	if len(extensionHandlers) > 1 {
		return false, errors.Errorf("found multiple CanUpdateMachine hooks (%s) (more than one is not supported yet)", strings.Join(extensionHandlers, ","))
	}

	canUpdateMachine, reasons, err := r.canExtensionsUpdateMachine(ctx, machine, machineUpToDateResult, extensionHandlers)
	if err != nil {
		return false, err
	}
	if !canUpdateMachine {
		log.Info(fmt.Sprintf("Machine cannot be updated in-place by extensions: %s", strings.Join(reasons, ",")), "Machine", klog.KObj(machine))
		return false, nil
	}
	return true, nil
}

// canExtensionsUpdateMachine calls CanUpdateMachine extensions to decide if a Machine can be updated in-place.
// Note: This is following the same general structure that is used in the Apply func in
// internal/controllers/topology/cluster/patches/engine.go.
func (r *KubeadmControlPlaneReconciler) canExtensionsUpdateMachine(ctx context.Context, machine *clusterv1.Machine, machineUpToDateResult internal.UpToDateResult, extensionHandlers []string) (bool, []string, error) {
	if r.overrideCanExtensionsUpdateMachine != nil {
		return r.overrideCanExtensionsUpdateMachine(ctx, machine, machineUpToDateResult, extensionHandlers)
	}

	log := ctrl.LoggerFrom(ctx)

	// Create the CanUpdateMachine request.
	req, err := createRequest(ctx, r.Client, machine, machineUpToDateResult)
	if err != nil {
		return false, nil, errors.Wrapf(err, "failed to generate CanUpdateMachine request")
	}

	var reasons []string
	for _, extensionHandler := range extensionHandlers {
		// Call CanUpdateMachine extension.
		resp := &runtimehooksv1.CanUpdateMachineResponse{}
		if err := r.RuntimeClient.CallExtension(ctx, runtimehooksv1.CanUpdateMachine, machine, extensionHandler, req, resp); err != nil {
			return false, nil, err
		}

		// Apply patches from the CanUpdateMachine response to the request.
		if err := applyPatchesToRequest(ctx, req, resp); err != nil {
			return false, nil, errors.Wrapf(err, "failed to apply patches from extension %s to the CanUpdateMachine request", extensionHandler)
		}

		// Check if current and desired objects are now matching.
		var matches bool
		matches, reasons, err = matchesMachine(req)
		if err != nil {
			return false, nil, errors.Wrapf(err, "failed to compare current and desired objects after calling extension %s", extensionHandler)
		}
		if matches {
			return true, nil, nil
		}
		log.V(5).Info(fmt.Sprintf("Machine cannot be updated in-place yet after calling extension %s: %s", extensionHandler, strings.Join(reasons, ",")), "Machine", klog.KObj(&req.Current.Machine))
	}

	return false, reasons, nil
}

func createRequest(ctx context.Context, c client.Client, currentMachine *clusterv1.Machine, machineUpToDateResult internal.UpToDateResult) (*runtimehooksv1.CanUpdateMachineRequest, error) {
	// DeepCopy objects to avoid mutations.
	currentMachineForDiff := currentMachine.DeepCopy()
	currentKubeadmConfigForDiff := machineUpToDateResult.CurrentKubeadmConfig.DeepCopy()
	currentInfraMachineForDiff := machineUpToDateResult.CurrentInfraMachine.DeepCopy()

	desiredMachineForDiff := machineUpToDateResult.DesiredMachine.DeepCopy()
	desiredKubeadmConfigForDiff := machineUpToDateResult.DesiredKubeadmConfig.DeepCopy()
	desiredInfraMachineForDiff := machineUpToDateResult.DesiredInfraMachine.DeepCopy()

	// Sync in-place mutable changes from desired to current KubeadmConfig / InfraMachine.
	// Note: Writing these fields is handled by syncMachines and not the responsibility of in-place updates.
	// Note: Desired KubeadmConfig / InfraMachine already contain the latest labels & annotations.
	currentKubeadmConfigForDiff.SetLabels(desiredKubeadmConfigForDiff.GetLabels())
	currentKubeadmConfigForDiff.SetAnnotations(desiredKubeadmConfigForDiff.GetAnnotations())
	currentInfraMachineForDiff.SetLabels(desiredInfraMachineForDiff.GetLabels())
	currentInfraMachineForDiff.SetAnnotations(desiredInfraMachineForDiff.GetAnnotations())

	// Apply defaulting to current / desired Machine / KubeadmConfig / InfraMachine.
	// Machine
	// Note: currentMachineForDiff doesn't need a dry-run as it was just written in syncMachines and then
	//       update in controlPlane to ensure the Machine we get here is the latest version of the Machine.
	// Note: desiredMachineForDiff needs a dry-run because otherwise we have unintended diffs, e.g. dataSecretName,
	//       providerID and nodeDeletionTimeout don't exist on the newly computed desired Machine.
	if err := ssa.Patch(ctx, c, kcpManagerName, desiredMachineForDiff, ssa.WithDryRun{}); err != nil {
		return nil, errors.Wrap(err, "server side apply dry-run failed for desired Machine")
	}
	// InfraMachine
	// Note: Both currentInfraMachineForDiff and desiredInfraMachineForDiff need a dry-run to ensure changes
	//       in defaulting logic and fields added by other controllers don't lead to an unintended diff.
	if err := ssa.Patch(ctx, c, kcpManagerName, currentInfraMachineForDiff, ssa.WithDryRun{}); err != nil {
		return nil, errors.Wrap(err, "server side apply dry-run failed for current InfraMachine")
	}
	if err := ssa.Patch(ctx, c, kcpManagerName, desiredInfraMachineForDiff, ssa.WithDryRun{}); err != nil {
		return nil, errors.Wrap(err, "server side apply dry-run failed for desired InfraMachine")
	}
	// KubeadmConfig
	// Note: Both currentKubeadmConfigForDiff and desiredKubeadmConfigForDiff don't need a dry-run as
	//       PrepareKubeadmConfigsForDiff already has to perfectly handle differences between current / desired
	//       KubeadmConfig. Otherwise the regular rollout logic would not detect correctly if a Machine needs a rollout.
	// Note: KubeadmConfig doesn't have a defaulting webhook and no API defaulting anymore.
	desiredKubeadmConfigForDiff, currentKubeadmConfigForDiff = internal.PrepareKubeadmConfigsForDiff(desiredKubeadmConfigForDiff, currentKubeadmConfigForDiff, true)

	// Cleanup objects and create request.
	req := &runtimehooksv1.CanUpdateMachineRequest{
		Current: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine: *cleanupMachine(currentMachineForDiff),
		},
		Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine: *cleanupMachine(desiredMachineForDiff),
		},
	}
	var err error
	req.Current.BootstrapConfig, err = convertToRawExtension(cleanupKubeadmConfig(currentKubeadmConfigForDiff))
	if err != nil {
		return nil, err
	}
	req.Desired.BootstrapConfig, err = convertToRawExtension(cleanupKubeadmConfig(desiredKubeadmConfigForDiff))
	if err != nil {
		return nil, err
	}
	req.Current.InfrastructureMachine, err = convertToRawExtension(cleanupUnstructured(currentInfraMachineForDiff))
	if err != nil {
		return nil, err
	}
	req.Desired.InfrastructureMachine, err = convertToRawExtension(cleanupUnstructured(desiredInfraMachineForDiff))
	if err != nil {
		return nil, err
	}

	return req, nil
}

func cleanupMachine(machine *clusterv1.Machine) *clusterv1.Machine {
	return &clusterv1.Machine{
		// Set GVK because object is later marshalled with json.Marshal.
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        machine.Name,
			Namespace:   machine.Namespace,
			Labels:      machine.Labels,
			Annotations: machine.Annotations,
		},
		Spec: *machine.Spec.DeepCopy(),
	}
}

func cleanupKubeadmConfig(kubeadmConfig *bootstrapv1.KubeadmConfig) *bootstrapv1.KubeadmConfig {
	return &bootstrapv1.KubeadmConfig{
		// Set GVK because object is later marshalled with json.Marshal.
		TypeMeta: metav1.TypeMeta{
			APIVersion: bootstrapv1.GroupVersion.String(),
			Kind:       "KubeadmConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kubeadmConfig.Name,
			Namespace:   kubeadmConfig.Namespace,
			Labels:      kubeadmConfig.Labels,
			Annotations: kubeadmConfig.Annotations,
		},
		Spec: *kubeadmConfig.Spec.DeepCopy(),
	}
}

func cleanupUnstructured(u *unstructured.Unstructured) *unstructured.Unstructured {
	cleanedUpU := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": u.GetAPIVersion(),
			"kind":       u.GetKind(),
			"spec":       u.Object["spec"],
		},
	}
	cleanedUpU.SetName(u.GetName())
	cleanedUpU.SetNamespace(u.GetNamespace())
	cleanedUpU.SetLabels(u.GetLabels())
	cleanedUpU.SetAnnotations(u.GetAnnotations())
	return cleanedUpU
}

func convertToRawExtension(object runtime.Object) (runtime.RawExtension, error) {
	objectBytes, err := json.Marshal(object)
	if err != nil {
		return runtime.RawExtension{}, errors.Wrap(err, "failed to marshal object to JSON")
	}

	objectUnstructured, ok := object.(*unstructured.Unstructured)
	if !ok {
		objectUnstructured = &unstructured.Unstructured{}
		// Note: This only succeeds if object has apiVersion & kind set (which is always the case).
		if err := json.Unmarshal(objectBytes, objectUnstructured); err != nil {
			return runtime.RawExtension{}, errors.Wrap(err, "failed to Unmarshal object into Unstructured")
		}
	}

	// Note: Raw and Object are always both set and Object is always set as an Unstructured
	//       to simplify subsequent code in matchesUnstructuredSpec.
	return runtime.RawExtension{
		Raw:    objectBytes,
		Object: objectUnstructured,
	}, nil
}

func applyPatchesToRequest(ctx context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) error {
	if resp.MachinePatch.IsDefined() {
		if err := applyPatchToMachine(ctx, &req.Current.Machine, resp.MachinePatch); err != nil {
			return err
		}
	}

	if resp.BootstrapConfigPatch.IsDefined() {
		if _, err := applyPatchToObject(ctx, &req.Current.BootstrapConfig, resp.BootstrapConfigPatch); err != nil {
			return err
		}
	}

	if resp.InfrastructureMachinePatch.IsDefined() {
		if _, err := applyPatchToObject(ctx, &req.Current.InfrastructureMachine, resp.InfrastructureMachinePatch); err != nil {
			return err
		}
	}

	return nil
}

func applyPatchToMachine(ctx context.Context, currentMachine *clusterv1.Machine, machinePath runtimehooksv1.Patch) error {
	// Note: Machine needs special handling because it is not a runtime.RawExtension. Simply converting it here to
	//       a runtime.RawExtension so we can avoid making the code in applyPatchToObject more complex.
	currentMachineRaw, err := convertToRawExtension(currentMachine)
	if err != nil {
		return err
	}

	machineChanged, err := applyPatchToObject(ctx, &currentMachineRaw, machinePath)
	if err != nil {
		return err
	}

	if !machineChanged {
		return nil
	}

	// Note: json.Unmarshal can't be used directly on *currentMachine as json.Unmarshal does not unset fields.
	patchedCurrentMachine := &clusterv1.Machine{}
	if err := json.Unmarshal(currentMachineRaw.Raw, patchedCurrentMachine); err != nil {
		return err
	}
	*currentMachine = *patchedCurrentMachine
	return nil
}

// applyPatchToObject applies the patch to the obj.
// Note: This is following the same general structure that is used in the applyPatchToRequest func in
// internal/controllers/topology/cluster/patches/engine.go.
func applyPatchToObject(ctx context.Context, obj *runtime.RawExtension, patch runtimehooksv1.Patch) (objChanged bool, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	if patch.PatchType == "" {
		return false, errors.Errorf("failed to apply patch: patchType is not set")
	}

	defer func() {
		if r := recover(); r != nil {
			log.Info(fmt.Sprintf("Observed a panic when applying patch: %v\n%s", r, string(debug.Stack())))
			reterr = kerrors.NewAggregate([]error{reterr, fmt.Errorf("observed a panic when applying patch: %v", r)})
		}
	}()

	// Create a copy of obj.Raw.
	// The patches will be applied to the copy and then only spec changes will be copied back to the obj.
	patchedObject := bytes.Clone(obj.Raw)
	var err error

	switch patch.PatchType {
	case runtimehooksv1.JSONPatchType:
		log.V(5).Info("Accumulating JSON patch", "patch", string(patch.Patch))
		jsonPatch, err := jsonpatch.DecodePatch(patch.Patch)
		if err != nil {
			log.Error(err, "Failed to apply patch: error decoding json patch (RFC6902)", "patch", string(patch.Patch))
			return false, errors.Wrap(err, "failed to apply patch: error decoding json patch (RFC6902)")
		}

		if len(jsonPatch) == 0 {
			// Return if there are no patches, nothing to do.
			return false, nil
		}

		patchedObject, err = jsonPatch.Apply(patchedObject)
		if err != nil {
			log.Error(err, "Failed to apply patch: error applying json patch (RFC6902)", "patch", string(patch.Patch))
			return false, errors.Wrap(err, "failed to apply patch: error applying json patch (RFC6902)")
		}
	case runtimehooksv1.JSONMergePatchType:
		if len(patch.Patch) == 0 || bytes.Equal(patch.Patch, []byte("{}")) {
			// Return if there are no patches, nothing to do.
			return false, nil
		}

		log.V(5).Info("Accumulating JSON merge patch", "patch", string(patch.Patch))
		patchedObject, err = jsonpatch.MergePatch(patchedObject, patch.Patch)
		if err != nil {
			log.Error(err, "Failed to apply patch: error applying json merge patch (RFC7386)", "patch", string(patch.Patch))
			return false, errors.Wrap(err, "failed to apply patch: error applying json merge patch (RFC7386)")
		}
	default:
		return false, errors.Errorf("failed to apply patch: unknown patchType %s", patch.PatchType)
	}

	// Overwrite the spec of obj with the spec of the patchedObject,
	// to ensure that we only pick up changes to the spec.
	if err := patchutil.PatchSpec(obj, patchedObject); err != nil {
		return false, errors.Wrap(err, "failed to apply patch to object")
	}

	return true, nil
}

func matchesMachine(req *runtimehooksv1.CanUpdateMachineRequest) (bool, []string, error) {
	var reasons []string
	match, diff, err := matchesMachineSpec(&req.Current.Machine, &req.Desired.Machine)
	if err != nil {
		return false, nil, errors.Wrapf(err, "failed to match Machine")
	}
	if !match {
		reasons = append(reasons, fmt.Sprintf("Machine cannot be updated in-place: %s", diff))
	}
	match, diff, err = matchesUnstructuredSpec(req.Current.BootstrapConfig, req.Desired.BootstrapConfig)
	if err != nil {
		return false, nil, errors.Wrapf(err, "failed to match KubeadmConfig")
	}
	if !match {
		reasons = append(reasons, fmt.Sprintf("KubeadmConfig cannot be updated in-place: %s", diff))
	}
	match, diff, err = matchesUnstructuredSpec(req.Current.InfrastructureMachine, req.Desired.InfrastructureMachine)
	if err != nil {
		return false, nil, errors.Wrapf(err, "failed to match %s", req.Current.InfrastructureMachine.Object.GetObjectKind().GroupVersionKind().Kind)
	}
	if !match {
		reasons = append(reasons, fmt.Sprintf("%s cannot be updated in-place: %s", req.Current.InfrastructureMachine.Object.GetObjectKind().GroupVersionKind().Kind, diff))
	}

	if len(reasons) > 0 {
		return false, reasons, nil
	}

	return true, nil, nil
}

func matchesMachineSpec(patched, desired *clusterv1.Machine) (equal bool, diff string, matchErr error) {
	// Note: Wrapping Machine specs in a Machine for proper formatting of the diff.
	return compare.Diff(
		&clusterv1.Machine{
			Spec: patched.Spec,
		},
		&clusterv1.Machine{
			Spec: desired.Spec,
		},
	)
}

func matchesUnstructuredSpec(patched, desired runtime.RawExtension) (equal bool, diff string, matchErr error) {
	// Note: Both patched and desired objects are always Unstructured as createRequest and
	//       applyPatchToObject are always setting objects as Unstructured.
	patchedUnstructured, ok := patched.Object.(*unstructured.Unstructured)
	if !ok {
		return false, "", errors.Errorf("patched object is not an Unstructured")
	}
	desiredUnstructured, ok := desired.Object.(*unstructured.Unstructured)
	if !ok {
		return false, "", errors.Errorf("desired object is not an Unstructured")
	}
	// Note: Wrapping Unstructured specs in an Unstructured for proper formatting of the diff.
	return compare.Diff(
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": patchedUnstructured.Object["spec"],
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": desiredUnstructured.Object["spec"],
			},
		},
	)
}
