package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const voicevoxBase = "http://localhost:50021"

type VoiceInfo struct {
	Name    string `json:"name"`
	StyleID int    `json:"style_id"`
	Display string `json:"display"`
}

type VoiceStore struct {
	mu     sync.RWMutex
	voices []VoiceInfo
}

func NewVoiceStore() *VoiceStore {
	return &VoiceStore{}
}

func (s *VoiceStore) Set(voices []VoiceInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voices = voices
}

func (s *VoiceStore) Get() []VoiceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]VoiceInfo, len(s.voices))
	copy(result, s.voices)
	return result
}

func (s *VoiceStore) FindSpeakerID(displayName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.voices {
		if v.Display == displayName {
			return v.StyleID
		}
	}
	return 1
}

type voicevoxSpeaker struct {
	Name   string          `json:"name"`
	Styles []voicevoxStyle `json:"styles"`
}

type voicevoxStyle struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetchVoices() ([]VoiceInfo, error) {
	resp, err := httpClient.Get(voicevoxBase + "/speakers")
	if err != nil {
		return nil, fmt.Errorf("fetch speakers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speakers status: %d", resp.StatusCode)
	}
	var speakers []voicevoxSpeaker
	if err := json.NewDecoder(resp.Body).Decode(&speakers); err != nil {
		return nil, fmt.Errorf("decode speakers: %w", err)
	}
	var voices []VoiceInfo
	for _, spk := range speakers {
		for _, style := range spk.Styles {
			display := spk.Name + " (" + style.Name + ")"
			voices = append(voices, VoiceInfo{
				Name:    spk.Name,
				StyleID: style.ID,
				Display: display,
			})
		}
	}
	return voices, nil
}

type audioQuery struct {
	SpeedScale  float64 `json:"speedScale"`
	VolumeScale float64 `json:"volumeScale"`
}

var synthSem = make(chan struct{}, 3)

type ChunkResult struct {
	WAV []byte
	Err error
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '.', '!', '?', '…':
		return true
	}
	return false
}

func splitSentences(text string) []string {
	var sentences []string
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isSentenceEnd(runes[i]) {
			continue
		}
		end := i
		for end+1 < len(runes) && isSentenceEnd(runes[end+1]) {
			end++
		}
		sentence := strings.TrimSpace(string(runes[start : end+1]))
		if sentence != "" {
			sentences = append(sentences, sentence)
			start = end + 1
		} else {
			start = end + 1
		}
		i = end
	}
	if start < len(runes) {
		remaining := strings.TrimSpace(string(runes[start:]))
		if remaining != "" {
			sentences = append(sentences, remaining)
		}
	}
	if len(sentences) == 0 {
		t := strings.TrimSpace(text)
		if t != "" {
			return []string{t}
		}
		return nil
	}
	return sentences
}

func synthesizeStream(text string, speakerID int, speed, volume float64) <-chan ChunkResult {
	sentences := splitSentences(text)
	out := make(chan ChunkResult, max(len(sentences), 1))

	if len(sentences) == 0 {
		close(out)
		return out
	}

	if len(sentences) == 1 {
		go func() {
			defer close(out)
			wav, err := synthesize(text, speakerID, speed, volume)
			out <- ChunkResult{wav, err}
		}()
		return out
	}

	type chunk struct {
		idx int
		wav []byte
		err error
	}
	ch := make(chan chunk, len(sentences))

	for i, s := range sentences {
		go func(idx int, sentence string) {
			synthSem <- struct{}{}
			defer func() { <-synthSem }()
			wav, err := synthesize(sentence, speakerID, speed, volume)
			ch <- chunk{idx, wav, err}
		}(i, s)
	}

	go func() {
		defer close(out)
		ordered := make([][]byte, len(sentences))
		next := 0
		for range sentences {
			c := <-ch
			if c.wav != nil {
				ordered[c.idx] = c.wav
			}
			for next < len(sentences) && ordered[next] != nil {
				out <- ChunkResult{ordered[next], nil}
				next++
			}
		}
		for next < len(sentences) {
			if ordered[next] != nil {
				out <- ChunkResult{ordered[next], nil}
			}
			next++
		}
	}()
	return out
}

func synthesize(text string, speakerID int, speed, volume float64) ([]byte, error) {
	params := url.Values{}
	params.Set("text", text)
	params.Set("speaker", strconv.Itoa(speakerID))
	queryURL := voicevoxBase + "/audio_query?" + params.Encode()
	queryResp, err := httpClient.Post(queryURL, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("audio_query: %w", err)
	}
	defer queryResp.Body.Close()
	if queryResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audio_query status: %d", queryResp.StatusCode)
	}

	body, err := io.ReadAll(queryResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read query: %w", err)
	}

	var q map[string]any
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}
	q["speedScale"] = speed
	q["volumeScale"] = volume

	modified, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	synthURL := voicevoxBase + "/synthesis?speaker=" + strconv.Itoa(speakerID)
	synthResp, err := httpClient.Post(synthURL, "application/json", bytes.NewReader(modified))
	if err != nil {
		return nil, fmt.Errorf("synthesis: %w", err)
	}
	defer synthResp.Body.Close()
	if synthResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("synthesis status: %d", synthResp.StatusCode)
	}

	wav, err := io.ReadAll(synthResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read wav: %w", err)
	}
	return wav, nil
}
