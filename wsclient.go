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
	Payload string   `json:"Payload"`
	Voice   *wsVoice `json:"Voice"`
}

type wsVoice struct {
	Name string `json:"Name"`
}

type messageHandler struct {
	config *ConfigStore
	voices *VoiceStore
}

func (h *messageHandler) handle(msg []byte) {
	var data wsMessage
	if err := json.Unmarshal(msg, &data); err != nil {
		log.Printf("[WS] JSON error: %v", err)
		return
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

	wav, err := synthesize(payload, speakerID, vc.Speed, vc.Volume)
	if err != nil {
		log.Printf("[VOICEVOX] synthesis error: %v", err)
		return
	}
	go func() {
		if err := playWAV(wav); err != nil {
			log.Printf("[PLAY] error: %v", err)
		}
	}()
}

func startWebSocket(config *ConfigStore, voices *VoiceStore, stop <-chan struct{}) {
	handler := &messageHandler{config: config, voices: voices}

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
