// Package conf 服务配置结构（docs/04 §1.8）。
package conf

import "github.com/tommax-bai/tommax-go-kit/objstore"

type Config struct {
	Server struct {
		HTTPAddr string `yaml:"httpAddr"`
	} `yaml:"server"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	DB struct {
		DSN string `yaml:"dsn"`
	} `yaml:"db"`
	Redis struct {
		Addr string `yaml:"addr"`
	} `yaml:"redis"`
	Adapter struct {
		Addr string `yaml:"addr"`
	} `yaml:"adapter"`
	ObjStore objstore.Config `yaml:"objstore"`
	Catalog  struct {
		Path string `yaml:"path"`
	} `yaml:"catalog"`
	Worker struct {
		Concurrency    int `yaml:"concurrency"`
		MaxRetry       int `yaml:"maxRetry"`
		PollIntervalMs int `yaml:"pollIntervalMs"`
		JobTimeoutSec  int `yaml:"jobTimeoutSec"`
	} `yaml:"worker"`
}
