package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	winmm      = syscall.NewLazyDLL("winmm.dll")
	playSoundW = winmm.NewProc("PlaySoundW")
)

const sndFilename = 0x00020000

func playWAV(data []byte) error {
	tmp, err := os.CreateTemp("", "tts-*.wav")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	ptr, err := syscall.UTF16PtrFromString(tmp.Name())
	if err != nil {
		return fmt.Errorf("utf16: %w", err)
	}
	ret, _, _ := playSoundW.Call(
		uintptr(unsafe.Pointer(ptr)),
		0,
		sndFilename,
	)
	if ret == 0 {
		return fmt.Errorf("PlaySoundW failed")
	}
	return nil
}
