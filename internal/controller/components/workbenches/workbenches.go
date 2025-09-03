package workbenches

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.WorkbenchesComponentName
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.Workbenches{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.WorkbenchesKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.WorkbenchesInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.Workbenches.ManagementState),
			},
		},
		Spec: componentApi.WorkbenchesSpec{
			WorkbenchesCommonSpec: dsc.Spec.Components.Workbenches.WorkbenchesCommonSpec,
		},
	}
}

func (s *componentHandler) Init(platform common.Platform) error {
	nbcManifestInfo := notebookControllerManifestInfo(notebookControllerManifestSourcePath)
	if err := odhdeploy.ApplyParams(nbcManifestInfo.String(), "params.env", map[string]string{
		"odh-notebook-controller-image": "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
		"oauth-proxy-image":             "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
	}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", nbcManifestInfo.String(), err)
	}

	kfNbcManifestInfo := kfNotebookControllerManifestInfo(kfNotebookControllerManifestSourcePath)
	if err := odhdeploy.ApplyParams(kfNbcManifestInfo.String(), "params.env", map[string]string{
		"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", kfNbcManifestInfo.String(), err)
	}

	nbImgsManifestInfo := notebookImagesManifestInfo(notebookImagesParamsPath)
	if err := odhdeploy.ApplyParams(nbImgsManifestInfo.String(), "params-latest.env", map[string]string{
		// CodeServer Workbench Images
		"odh-workbench-codeserver-datascience-cpu-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_CODESERVER_DATASCIENCE_CPU_PY311_IMAGE",
		"odh-workbench-codeserver-datascience-cpu-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_CODESERVER_DATASCIENCE_CPU_PY312_IMAGE",

		// Jupyter Workbench Images - Data Science CPU
		"odh-workbench-jupyter-datascience-cpu-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_DATASCIENCE_CPU_PY311_IMAGE",
		"odh-workbench-jupyter-datascience-cpu-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_DATASCIENCE_CPU_PY312_IMAGE",

		// Jupyter Workbench Images - Minimal CPU
		"odh-workbench-jupyter-minimal-cpu-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CPU_PY311_IMAGE",
		"odh-workbench-jupyter-minimal-cpu-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CPU_PY312_IMAGE",
		// Jupyter Workbench Images - Minimal CUDA
		"odh-workbench-jupyter-minimal-cuda-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CUDA_PY311_IMAGE",
		"odh-workbench-jupyter-minimal-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CUDA_PY312_IMAGE",
		// Jupyter Workbench Images - Minimal ROCm
		"odh-workbench-jupyter-minimal-rocm-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_ROCM_PY311_IMAGE",
		"odh-workbench-jupyter-minimal-rocm-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_ROCM_PY312_IMAGE",

		// Jupyter Workbench Images - PyTorch CUDA
		"odh-workbench-jupyter-pytorch-cuda-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_CUDA_PY311_IMAGE",
		"odh-workbench-jupyter-pytorch-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_CUDA_PY312_IMAGE",
		// Jupyter Workbench Images - PyTorch ROCm
		"odh-workbench-jupyter-pytorch-rocm-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_ROCM_PY311_IMAGE",
		"odh-workbench-jupyter-pytorch-rocm-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_ROCM_PY312_IMAGE",

		// Jupyter Workbench Images - TensorFlow CUDA
		"odh-workbench-jupyter-tensorflow-cuda-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TENSORFLOW_CUDA_PY311_IMAGE",
		"odh-workbench-jupyter-tensorflow-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TENSORFLOW_CUDA_PY312_IMAGE",
		// Jupyter Workbench Images - TensorFlow ROCm
		"odh-workbench-jupyter-tensorflow-rocm-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TENSORFLOW_ROCM_PY311_IMAGE",

		// Jupyter Workbench Images - TrustyAI CPU
		"odh-workbench-jupyter-trustyai-cpu-py311-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TRUSTYAI_CPU_PY311_IMAGE",
		"odh-workbench-jupyter-trustyai-cpu-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TRUSTYAI_CPU_PY312_IMAGE",

		// Jupyter Workbench Images - PyTorch+llmcompressor CUDA
		"odh-workbench-jupyter-pytorch-llmcompressor-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_LLMCOMPRESSOR_CUDA_PY312_IMAGE",

		// Pipeline Runtime Images
		"odh-pipeline-runtime-datascience-cpu-py311-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_DATASCIENCE_CPU_PY311_IMAGE",
		"odh-pipeline-runtime-datascience-cpu-py312-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_DATASCIENCE_CPU_PY312_IMAGE",
		"odh-pipeline-runtime-minimal-cpu-py311-ubi9-n":     "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_MINIMAL_CPU_PY311_IMAGE",
		"odh-pipeline-runtime-minimal-cpu-py312-ubi9-n":     "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_MINIMAL_CPU_PY312_IMAGE",
		"odh-pipeline-runtime-tensorflow-cuda-py311-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_TENSORFLOW_CUDA_PY311_IMAGE",
		"odh-pipeline-runtime-tensorflow-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_TENSORFLOW_CUDA_PY312_IMAGE",
		"odh-pipeline-runtime-tensorflow-rocm-py311-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_TENSORFLOW_ROCM_PY311_IMAGE",
		// Pipeline Runtime Images - PyTorch CUDA
		"odh-pipeline-runtime-pytorch-cuda-py311-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_CUDA_PY311_IMAGE",
		"odh-pipeline-runtime-pytorch-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_CUDA_PY312_IMAGE",
		// Pipeline Runtime Images - PyTorch ROCm
		"odh-pipeline-runtime-pytorch-rocm-py311-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_ROCM_PY311_IMAGE",
		"odh-pipeline-runtime-pytorch-rocm-py312-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_ROCM_PY312_IMAGE",
		// Pipeline Runtime Images - PyTorch+llmcompressor CUDA
		"odh-pipeline-runtime-pytorch-llmcompressor-cuda-py312-ubi9-n": "RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_LLMCOMPRESSOR_CUDA_PY312_IMAGE",
	}); err != nil {
		return fmt.Errorf("failed to update params-latest.env from %s : %w", nbImgsManifestInfo.String(), err)
	}

	return nil
}

func (s *componentHandler) IsEnabled(dsc *dscv1.DataScienceCluster) bool {
	return dsc.Spec.Components.Workbenches.ManagementState == operatorv1.Managed
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.Workbenches{}
	c.Name = componentApi.WorkbenchesInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.Workbenches.ManagementState)

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.Workbenches.ManagementState = ms
	dsc.Status.Components.Workbenches.WorkbenchesCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.Workbenches.WorkbenchesCommonStatus = c.Status.WorkbenchesCommonStatus.DeepCopy()

		if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, *rc)
			cs = rc.Status
		} else {
			cs = metav1.ConditionFalse
		}
	} else {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(string(ms)),
			conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)
	}

	return cs, nil
}
