package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

type statusInfo struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

type serverState struct {
	connected atomic.Bool
	voicesOk  atomic.Bool
	config    *ConfigStore
	voices    *VoiceStore
	msgLog    *MessageLog
	sseBroker *SSEBroker
	debugCfg  *DebugConfig
}

func newServer(config *ConfigStore, voices *VoiceStore, msgLog *MessageLog, sseBroker *SSEBroker, debugCfg *DebugConfig) *serverState {
	return &serverState{config: config, voices: voices, msgLog: msgLog, sseBroker: sseBroker, debugCfg: debugCfg}
}

func (s *serverState) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.serveUI)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config", s.setConfig)
	mux.HandleFunc("GET /api/voices", s.getVoices)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/events", s.sseHandler)
	mux.HandleFunc("GET /api/debug", s.getDebug)
	mux.HandleFunc("POST /api/debug", s.setDebug)
	return mux
}

func (s *serverState) serveUI(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *serverState) getConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.config.GetAll())
}

func (s *serverState) setConfig(w http.ResponseWriter, r *http.Request) {
	var data ConfigData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.config.SetAll(data)
	if err := s.config.Save(); err != nil {
		log.Printf("[CONFIG] save error: %v", err)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *serverState) getVoices(w http.ResponseWriter, r *http.Request) {
	v, err := fetchVoices()
	if err != nil {
		s.voicesOk.Store(false)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.voices.Set(v)
	s.voicesOk.Store(true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *serverState) getStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	info := statusInfo{Text: "Ready", Type: ""}
	if !s.voicesOk.Load() {
		info = statusInfo{Text: "VOICEVOX unavailable", Type: "error"}
	} else if s.connected.Load() {
		info = statusInfo{Text: "Connected to TextToTalk", Type: "success"}
	}
	json.NewEncoder(w).Encode(info)
}

func (s *serverState) sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.sseBroker.Subscribe()
	defer s.sseBroker.Unsubscribe(ch)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *serverState) getDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.debugCfg.Get())
}

func (s *serverState) setDebug(w http.ResponseWriter, r *http.Request) {
	var cfg DebugConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.debugCfg.Set(&cfg)
	w.WriteHeader(http.StatusOK)
}
