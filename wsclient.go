package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const wsURL = "ws://127.0.0.1:5000/Messages"

type wsMessage struct {
	Type    string   `json:"Type"`
	Speaker string   `json:"Speaker"`
	Payload string   `json:"Payload"`
	Voice   *wsVoice `json:"Voice"`
}

type wsVoice struct {
	Name string `json:"Name"`
}

type messageHandler struct {
	config    *ConfigStore
	voices    *VoiceStore
	msgLog    *MessageLog
	sseBroker *SSEBroker
	debugCfg  *DebugConfig
}

func (h *messageHandler) handle(msg []byte) {
	var data wsMessage
	if err := json.Unmarshal(msg, &data); err != nil {
		log.Printf("[WS] JSON error: %v", err)
		return
	}

	if h.debugCfg.Get().ShowRaw {
		log.Printf("[RAW] %s", string(msg))
	}

	if data.Type != "Say" {
		return
	}
	payload := data.Payload
	if payload == "" {
		return
	}
	voiceName := "Unknown"
	if data.Voice != nil {
		voiceName = data.Voice.Name
	}
	var category string
	switch voiceName {
	case "Male":
		category = "male"
	case "Female":
		category = "female"
	default:
		category = "unknown"
	}

	vc := h.config.Get(category)
	speakerID := h.voices.FindSpeakerID(vc.VoiceDisplay)

	log.Printf("[TTS] (%s) %s", voiceName, payload)

	genderLabel := "？"
	switch category {
	case "male":
		genderLabel = "男"
	case "female":
		genderLabel = "女"
	}

	speakerName := data.Speaker
	if speakerName == "" {
		speakerName = voiceName
	}
	displayVoice := speakerName + "（" + genderLabel + "）"

	chatMsg := ChatMessage{
		Timestamp: time.Now().Format("15:04:05"),
		Voice:     displayVoice,
		Category:  category,
		Text:      payload,
	}
	msgID := h.msgLog.Add(chatMsg)
	h.sseBroker.PublishEvent("message", chatMsg)

	go func() {
		start := time.Now()
		var chunks []ChunkInfo
		for c := range synthesizeStream(payload, speakerID, vc.Speed, vc.Volume) {
			if c.Err != nil {
				log.Printf("[VOICEVOX] synthesis error: %v", c.Err)
				continue
			}
			chunks = append(chunks, ChunkInfo{Text: c.Text, TimeMs: c.SynthMs})
			if err := playWAV(c.WAV); err != nil {
				log.Printf("[PLAY] error: %v", err)
			}
		}
		totalMs := time.Since(start).Milliseconds()
		h.msgLog.UpdateChunks(msgID, chunks, totalMs)
		h.sseBroker.PublishEvent("chunks", map[string]interface{}{
			"id":       msgID,
			"chunks":   chunks,
			"total_ms": totalMs,
		})
		if h.debugCfg.Get().ShowChunks {
			log.Printf("[CHUNKS] #%d: %d chunks, %dms total", msgID, len(chunks), totalMs)
			for i, c := range chunks {
				log.Printf("[CHUNK %d/%d] synth %dms: %s", i+1, len(chunks), c.TimeMs, c.Text)
			}
		}
	}()
}

func startWebSocket(config *ConfigStore, voices *VoiceStore, msgLog *MessageLog, sseBroker *SSEBroker, debugCfg *DebugConfig, stop <-chan struct{}) {
	handler := &messageHandler{config: config, voices: voices, msgLog: msgLog, sseBroker: sseBroker, debugCfg: debugCfg}

	for {
		select {
		case <-stop:
			return
		default:
		}

		log.Printf("[WS] Connecting to %s ...", wsURL)
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			log.Printf("[WS] Connection failed: %v, retrying in 5s", err)
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		log.Printf("[WS] Connected to TextToTalk")

		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, message, err := c.ReadMessage()
				if err != nil {
					log.Printf("[WS] Read error: %v", err)
					return
				}
				handler.handle(message)
			}
		}()

		select {
		case <-stop:
			c.Close()
			<-done
			return
		case <-done:
			c.Close()
			log.Printf("[WS] Disconnected, reconnecting in 5s")
			time.Sleep(5 * time.Second)
		}
	}
}
