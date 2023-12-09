package config

import (
	"github.com/spf13/viper"
	"log"
	"path/filepath"
	"strings"
)

type Loader struct {
	BaseConfig string
	Landscape  string
}

func (l *Loader) Load() (RootConfig, error) {

	basePath, err := filepath.Abs(l.BaseConfig)
	if err != nil {
		log.Println("🚨error getting absolute path for base config", err)
		return RootConfig{}, err
	}

	replacer := strings.NewReplacer(".", "__")

	// load base file
	base := viper.New()
	base.SetConfigName("settings")
	base.SetConfigType("yaml")
	base.AddConfigPath(basePath)

	base.AutomaticEnv()
	base.SetEnvPrefix("atomi")
	base.SetEnvKeyReplacer(replacer)

	err = base.ReadInConfig()
	if err != nil {
		log.Println("🚨error reading base config", err)
		return RootConfig{}, err
	}

	// load landscape file
	landscape := viper.New()

	landscape.SetConfigName("settings." + l.Landscape)
	landscape.SetConfigType("yaml")
	landscape.AddConfigPath(basePath)

	landscape.AutomaticEnv()
	landscape.SetEnvPrefix("atomi")
	landscape.SetEnvKeyReplacer(replacer)

	err = landscape.ReadInConfig()

	if err != nil {
		log.Println("🚨error reading landscape config", err)
		return RootConfig{}, err
	}

	// merge configs
	err = base.MergeConfigMap(landscape.AllSettings())
	if err != nil {
		log.Println("🚨error merging base with landscape config", err)
		return RootConfig{}, err
	}
	var c RootConfig

	err = base.Unmarshal(&c)
	return c, err
}
