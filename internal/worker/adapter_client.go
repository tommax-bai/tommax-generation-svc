// adapter_client.go 是对 model-adapter-svc 的防腐层（docs/03 §1.1）：
// proto 类型只活在本文件，向 worker 暴露本域的小类型。
package worker

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	modeladapterv1 "github.com/tommax-bai/tommax-proto/gen/go/tommax/modeladapter/v1"
)

type AdapterClient struct {
	cli modeladapterv1.InferenceServiceClient
}

func NewAdapterClient(addr string) (*AdapterClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial model-adapter %s: %w", addr, err)
	}
	return &AdapterClient{cli: modeladapterv1.NewInferenceServiceClient(conn)}, nil
}

// capability 映射：generation task_type → adapter Capability。
var capabilityByTaskType = map[string]modeladapterv1.Capability{
	"image.text2img":   modeladapterv1.Capability_CAPABILITY_IMAGE_TEXT2IMG,
	"image.ref2img":    modeladapterv1.Capability_CAPABILITY_IMAGE_REF2IMG,
	"video.text2video": modeladapterv1.Capability_CAPABILITY_VIDEO_TEXT2VIDEO,
	"video.img2video":  modeladapterv1.Capability_CAPABILITY_VIDEO_IMG2VIDEO,
}

type SubmitSpec struct {
	TaskID        string
	ProviderModel string
	TaskType      string
	Prompt        string
	RefURLs       []string
	Params        map[string]string
}

type JobStatus string

const (
	JobRunning         JobStatus = "RUNNING"
	JobSucceeded       JobStatus = "SUCCEEDED"
	JobFailedRetryable JobStatus = "FAILED_RETRYABLE"
	JobFailedPermanent JobStatus = "FAILED_PERMANENT"
	JobContentBlocked  JobStatus = "CONTENT_BLOCKED"
)

type JobOutput struct {
	URL      string
	Data     []byte
	MimeType string
	Width    int
	Height   int
}

type JobSnapshot struct {
	Status   JobStatus
	Progress int
	Outputs  []JobOutput
	ErrorMsg string
}

func (a *AdapterClient) Submit(ctx context.Context, spec SubmitSpec) (string, error) {
	cap, ok := capabilityByTaskType[spec.TaskType]
	if !ok {
		return "", fmt.Errorf("task_type %q has no adapter capability mapping", spec.TaskType)
	}
	resp, err := a.cli.Submit(ctx, &modeladapterv1.SubmitRequest{
		TaskId:        spec.TaskID,
		ProviderModel: spec.ProviderModel,
		Capability:    cap,
		Prompt:        spec.Prompt,
		RefUrls:       spec.RefURLs,
		Params:        spec.Params,
	})
	if err != nil {
		return "", fmt.Errorf("adapter submit: %w", err)
	}
	return resp.GetJobId(), nil
}

func (a *AdapterClient) Query(ctx context.Context, jobID string) (*JobSnapshot, error) {
	resp, err := a.cli.Query(ctx, &modeladapterv1.QueryRequest{JobId: jobID})
	if err != nil {
		return nil, fmt.Errorf("adapter query %s: %w", jobID, err)
	}
	snap := &JobSnapshot{Progress: int(resp.GetProgress()), ErrorMsg: resp.GetErrorMessage()}
	switch resp.GetStatus() {
	case modeladapterv1.JobStatus_JOB_STATUS_SUCCEEDED:
		snap.Status = JobSucceeded
	case modeladapterv1.JobStatus_JOB_STATUS_FAILED_RETRYABLE:
		snap.Status = JobFailedRetryable
	case modeladapterv1.JobStatus_JOB_STATUS_FAILED_PERMANENT:
		snap.Status = JobFailedPermanent
	case modeladapterv1.JobStatus_JOB_STATUS_CONTENT_BLOCKED:
		snap.Status = JobContentBlocked
	default:
		snap.Status = JobRunning
	}
	for _, o := range resp.GetOutputs() {
		snap.Outputs = append(snap.Outputs, JobOutput{
			URL: o.GetUrl(), Data: o.GetData(), MimeType: o.GetMimeType(),
			Width: int(o.GetWidth()), Height: int(o.GetHeight()),
		})
	}
	return snap, nil
}

func (a *AdapterClient) Cancel(ctx context.Context, jobID string) {
	_, _ = a.cli.Cancel(ctx, &modeladapterv1.CancelRequest{JobId: jobID})
}
