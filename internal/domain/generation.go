// Package domain 是生成任务中心的领域层：实体、状态机、仓储接口与领域错误。
// 本包不 import 任何框架/驱动/proto（docs/03 §1.1 铁律；errs 为自研纯基建包，属领域错误定义手段）。
package domain

import (
	"context"
	"time"

	"github.com/tommax-bai/tommax-go-kit/errs"
)

// Status 任务状态机：PENDING → QUEUED → RUNNING → SUCCEEDED | FAILED | CANCELED。
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCanceled  Status = "CANCELED"
)

// Terminal 报告状态是否终态。
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

// Cancelable 报告能否从当前状态取消。
func (s Status) Cancelable() bool {
	return s == StatusPending || s == StatusQueued || s == StatusRunning
}

// Output 任务产物。
type Output struct {
	AssetURL string `json:"assetUrl"`
	MimeType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Task 生成任务实体。
type Task struct {
	ID            int64
	UserID        string
	RequestID     string // 客户端幂等键
	TaskType      string // docs/02 任务类型全集，如 image.text2img
	ModelKey      string
	Prompt        string
	RefAssetURLs  []string
	Params        map[string]string
	CanvasCtx     map[string]string
	Status        Status
	Progress      int
	Outputs       []Output
	ErrorReason   string
	ProviderJobID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TaskRepo 仓储接口（定义在消费方，repo 层实现）。
type TaskRepo interface {
	Create(ctx context.Context, t *Task) error
	GetByID(ctx context.Context, id int64) (*Task, error)
	GetByUserRequestID(ctx context.Context, userID, requestID string) (*Task, error)
	// List 返回 userID 名下 id < beforeID 的最近任务（beforeID<=0 表示从头），按 id 降序。
	List(ctx context.Context, userID, taskType string, limit int, beforeID int64) ([]*Task, error)
	// CASStatus 原子状态迁移，返回是否迁移成功（worker 防重复执行的关键）。
	CASStatus(ctx context.Context, id int64, from, to Status) (bool, error)
	SaveProgress(ctx context.Context, id int64, progress int) error
	SaveProviderJob(ctx context.Context, id int64, jobID string) error
	// Finish 落终态。
	Finish(ctx context.Context, id int64, status Status, outputs []Output, errorReason string) error
}

// ModelInfo 模型目录条目（Phase 1 由配置文件下发，docs/05 §1.1）。
type ModelInfo struct {
	Key              string   `yaml:"key" json:"key"`
	Label            string   `yaml:"label" json:"label"`
	Capability       string   `yaml:"capability" json:"capability"`
	ProviderModel    string   `yaml:"providerModel" json:"-"`
	Description      string   `yaml:"description" json:"description"`
	Tags             []string `yaml:"tags" json:"tags"`
	ParamsSchemaJSON string   `yaml:"paramsSchema" json:"paramsSchemaJson"`
}

// Catalog 模型目录只读接口。
type Catalog interface {
	Get(key string) (ModelInfo, bool)
	List() []ModelInfo
}

// 生成域错误（域编号 13，注册规则见 docs/04 §1.6）。
var (
	ErrTaskNotFound     = errs.New(44, errs.DomainGeneration, 1, "GENERATION_NOT_FOUND", "任务不存在")
	ErrModelOffline     = errs.New(40, errs.DomainGeneration, 2, "GENERATION_MODEL_OFFLINE", "模型不存在或已下线")
	ErrTaskTypeMismatch = errs.New(40, errs.DomainGeneration, 3, "GENERATION_TASK_TYPE_MISMATCH", "任务类型与模型能力不匹配")
	ErrNotCancelable    = errs.New(40, errs.DomainGeneration, 4, "GENERATION_NOT_CANCELABLE", "任务已结束，无法取消")
	ErrPromptRequired   = errs.New(40, errs.DomainGeneration, 5, "GENERATION_PROMPT_REQUIRED", "请填写提示词")
)
