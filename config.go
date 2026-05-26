package main

import (
	"encoding/json"
	"os"
	"sync"
)

const configFile = "config.json"

type VoiceConfig struct {
	VoiceDisplay string  `json:"voice_display"`
	Speed        float64 `json:"speed"`
	Volume       float64 `json:"volume"`
}

type ConfigData struct {
	Male    VoiceConfig `json:"male"`
	Female  VoiceConfig `json:"female"`
	Unknown VoiceConfig `json:"unknown"`
}

type ConfigStore struct {
	mu   sync.RWMutex
	data ConfigData
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{
		data: ConfigData{
			Male:    VoiceConfig{Speed: 1.0, Volume: 1.0},
			Female:  VoiceConfig{Speed: 1.0, Volume: 1.0},
			Unknown: VoiceConfig{Speed: 1.0, Volume: 1.0},
		},
	}
}

func (s *ConfigStore) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(configFile)
	if err != nil {
		return
	}
	var loaded ConfigData
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	if loaded.Male.Speed == 0 {
		loaded.Male.Speed = 1.0
	}
	if loaded.Female.Speed == 0 {
		loaded.Female.Speed = 1.0
	}
	if loaded.Unknown.Speed == 0 {
		loaded.Unknown.Speed = 1.0
	}
	s.data = loaded
}

func (s *ConfigStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jsonData, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, jsonData, 0644)
}

func (s *ConfigStore) Get(category string) VoiceConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch category {
	case "male":
		return s.data.Male
	case "female":
		return s.data.Female
	default:
		return s.data.Unknown
	}
}

func (s *ConfigStore) Set(category string, vc VoiceConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch category {
	case "male":
		s.data.Male = vc
	case "female":
		s.data.Female = vc
	default:
		s.data.Unknown = vc
	}
}

func (s *ConfigStore) GetAll() ConfigData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

func (s *ConfigStore) SetAll(data ConfigData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
}
