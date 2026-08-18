package repo

import (
	"fmt"

	"github.com/tommax-bai/tommax-go-kit/configx"

	"github.com/tommax-bai/tommax-generation-svc/internal/domain"
)

// CatalogFile 是模型目录的配置文件实现（docs/05 §1.1：admin-svc 延后，目录随发布生效）。
type CatalogFile struct {
	byKey map[string]domain.ModelInfo
	list  []domain.ModelInfo
}

type catalogYAML struct {
	Models []domain.ModelInfo `yaml:"models"`
}

func NewCatalogFile(path string) (*CatalogFile, error) {
	var raw catalogYAML
	if err := configx.Load(path, &raw); err != nil {
		return nil, fmt.Errorf("load model catalog: %w", err)
	}
	c := &CatalogFile{byKey: make(map[string]domain.ModelInfo, len(raw.Models)), list: raw.Models}
	for _, m := range raw.Models {
		if m.Key == "" || m.Capability == "" || m.ProviderModel == "" {
			return nil, fmt.Errorf("model catalog entry invalid: %+v", m)
		}
		c.byKey[m.Key] = m
	}
	return c, nil
}

func (c *CatalogFile) Get(key string) (domain.ModelInfo, bool) {
	m, ok := c.byKey[key]
	return m, ok
}

func (c *CatalogFile) List() []domain.ModelInfo { return c.list }
