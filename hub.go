package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

type ChunkInfo struct {
	Text   string `json:"text"`
	TimeMs int64  `json:"time_ms"`
}

type ChatMessage struct {
	ID        int         `json:"id"`
	Timestamp string      `json:"timestamp"`
	Voice     string      `json:"voice"`
	Category  string      `json:"category"`
	Text      string      `json:"text"`
	Chunks    []ChunkInfo `json:"chunks,omitempty"`
	TotalMs   int64       `json:"total_ms,omitempty"`
}

type MessageLog struct {
	mu       sync.Mutex
	messages []ChatMessage
	nextID   int
}

func NewMessageLog() *MessageLog {
	return &MessageLog{
		messages: make([]ChatMessage, 0, 200),
		nextID:   1,
	}
}

func (l *MessageLog) Add(msg ChatMessage) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg.ID = l.nextID
	l.nextID++
	l.messages = append(l.messages, msg)
	if len(l.messages) > 200 {
		copy(l.messages, l.messages[len(l.messages)-200:])
		l.messages = l.messages[:200]
	}
	return msg.ID
}

func (l *MessageLog) UpdateChunks(id int, chunks []ChunkInfo, totalMs int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.messages {
		if l.messages[i].ID == id {
			l.messages[i].Chunks = chunks
			l.messages[i].TotalMs = totalMs
			return
		}
	}
}

type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan string]struct{}),
	}
}

func (b *SSEBroker) Subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *SSEBroker) Publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *SSEBroker) PublishEvent(event string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.Publish(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(data)))
}

type DebugConfig struct {
	mu         sync.RWMutex
	ShowChunks bool `json:"show_chunks"`
	ShowTiming bool `json:"show_timing"`
	ShowRaw    bool `json:"show_raw"`
}

func NewDebugConfig() *DebugConfig {
	return &DebugConfig{}
}

func (d *DebugConfig) Get() DebugConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return DebugConfig{
		ShowChunks: d.ShowChunks,
		ShowTiming: d.ShowTiming,
		ShowRaw:    d.ShowRaw,
	}
}

func (d *DebugConfig) Set(cfg *DebugConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ShowChunks = cfg.ShowChunks
	d.ShowTiming = cfg.ShowTiming
	d.ShowRaw = cfg.ShowRaw
}
