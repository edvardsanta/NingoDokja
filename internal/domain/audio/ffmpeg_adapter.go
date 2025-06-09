package domain

import (
	"encoding/binary"
	"errors"
	"fmt"

	"io"
	"log"
	"math"
	"os/exec"
	"sync"
)

type FFmpegAdapter struct {
	cmd           *exec.Cmd
	encoder       AudioEncoder
	stopChan      chan struct{}
	stopMutex     sync.Mutex
	currentVolume int

	EncodedPackets chan []byte // canal que emite pacotes codificados
}

func NewFFmpegAdapter(encoder AudioEncoder) *FFmpegAdapter {
	return &FFmpegAdapter{
		encoder:        encoder,
		stopChan:       make(chan struct{}),
		EncodedPackets: make(chan []byte, 100), // buffer para não travar
		currentVolume:  100,
	}
}

func (f *FFmpegAdapter) StreamAudio(source string) error {
	f.resetStopChan()
	args := []string{
		"-i", source,
		"-f", "s16le", // formato raw PCM
		"-ar", fmt.Sprintf("%d", f.encoder.SampleRate()), // sample rate do encoder
		"-ac", fmt.Sprintf("%d", f.encoder.Channels()), // canais do encoder
		"pipe:1",
	}
	f.cmd = exec.Command("ffmpeg", args...)

	stdout, err := f.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error getting ffmpeg stdout: %w", err)
	}

	if err := f.cmd.Start(); err != nil {
		return fmt.Errorf("error starting ffmpeg: %w", err)
	}

	go f.processAudioStream(stdout)

	return nil
}

func (f *FFmpegAdapter) Stop() error {
	close(f.stopChan)
	if f.cmd != nil && f.cmd.Process != nil {
		err := f.cmd.Process.Kill()
		if err != nil {
			return err
		}
	}
	close(f.EncodedPackets)
	return nil
}

func (f *FFmpegAdapter) processAudioStream(stdout io.Reader) {
	pcmBuf := make([]int16, f.encoder.FrameSamples())
	for {
		select {
		case <-f.stopChan:
			return
		default:
			if err := binary.Read(stdout, binary.LittleEndian, &pcmBuf); err != nil {
				if err == io.EOF {
					return
				}
				log.Println("Error reading PCM data:", err)
				return
			}

			pcm := f.applyVolume(pcmBuf, f.currentVolume)

			encoded, err := f.encoder.Encode(pcm)
			if err != nil {
				log.Println("Error encoding audio:", err)
				continue
			}
			f.EncodedPackets <- encoded
		}
	}
}

func (f *FFmpegAdapter) SetVolume(volume int) error {
	if volume < 0 || volume > 100 {
		return errors.New("volume out of range")
	}
	f.currentVolume = volume
	return nil
}

func (f *FFmpegAdapter) applyVolume(pcm []int16, volume int) []int16 {
	if volume == 100 {
		return pcm
	}
	adjusted := make([]int16, len(pcm))
	vol := float64(volume) / 100.0
	for i, sample := range pcm {
		adj := float64(sample) * vol
		if adj > math.MaxInt16 {
			adj = math.MaxInt16
		} else if adj < math.MinInt16 {
			adj = math.MinInt16
		}
		adjusted[i] = int16(adj)
	}
	return adjusted
}

func (f *FFmpegAdapter) resetStopChan() {
	f.stopMutex.Lock()
	defer f.stopMutex.Unlock()
	if f.stopChan != nil {
		close(f.stopChan)
	}
	f.stopChan = make(chan struct{})
}
