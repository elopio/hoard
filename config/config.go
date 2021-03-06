package config

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/monax/hoard/config/logging"
	"github.com/monax/hoard/config/storage"
)

const (
	DefaultFileName      = "hoard.toml"
	DefaultListenAddress = "tcp://localhost:53431"
)

var DefaultHoardConfig = NewHoardConfig(DefaultListenAddress,
	storage.DefaultConfig, logging.DefaultConfig)

type HoardConfig struct {
	ListenAddress string
	Storage       *storage.StorageConfig
	Logging       *logging.LoggingConfig
	// TODO: SecretsConfig - how to access bootstrapping secrets
}

func NewHoardConfig(listenAddress string, storageConfig *storage.StorageConfig,
	loggingConfig *logging.LoggingConfig) *HoardConfig {
	return &HoardConfig{
		ListenAddress: listenAddress,
		Storage:       storageConfig,
		Logging:       loggingConfig,
	}
}

func HoardConfigFromString(tomlString string) (*HoardConfig, error) {
	hoardConfig := new(HoardConfig)
	_, err := toml.Decode(tomlString, hoardConfig)
	if err != nil {
		return nil, err
	}
	return hoardConfig, nil
}

func (hoardConfig *HoardConfig) TOMLString() string {
	buf := new(bytes.Buffer)
	encoder := toml.NewEncoder(buf)
	err := encoder.Encode(hoardConfig)
	if err != nil {
		return fmt.Sprintf("<Could not serialise HoardConfig>")
	}
	return buf.String()
}
