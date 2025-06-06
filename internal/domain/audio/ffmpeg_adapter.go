package domain

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
	"io"
	"log"
	"math"
	"os/exec"
	"sync"
	"time"
)

type FFmpegAdapter struct {
	cmd           *exec.Cmd
	opusEncoder   *opus.Encoder
	stopChan      chan struct{}
	stopMutex     sync.Mutex
	currentVolume map[string]int
}

func NewFFmpegAdapter() *FFmpegAdapter {
	encoder, _ := opus.NewEncoder(48000, 2, opus.Application(2049))
	return &FFmpegAdapter{
		opusEncoder:   encoder,
		stopChan:      make(chan struct{}),
		currentVolume: make(map[string]int),
	}
}

func (f *FFmpegAdapter) StreamAudio(vc *discordgo.VoiceConnection, source string) error {
	// TODO: Try to decouple this from discordgo
	// sincerely i think it is not possible
	f.resetStopChan()
	f.cmd = exec.Command("ffmpeg", "-i", source, "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:1")

	stdout, err := f.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error getting ffmpeg stdout: %w", err)
	}

	if err := f.cmd.Start(); err != nil {
		return fmt.Errorf("error starting ffmpeg: %w", err)
	}

	return f.processAudioStream(vc, stdout)
}

func (f *FFmpegAdapter) SetVolume(vc *discordgo.VoiceConnection, volume int) error {
	if vc == nil {
		return errors.New("voice connection is nil")
	}
	if volume < 0 || volume > 100 {
		return errors.New("volume out of range")
	}
	f.currentVolume[vc.GuildID] = volume
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
	return nil
}

func (f *FFmpegAdapter) processAudioStream(vc *discordgo.VoiceConnection, stdout io.Reader) error {
	// TODO: Do error handling
	vc.Speaking(true)
	defer vc.Speaking(false)

	opusEncoder, err := opus.NewEncoder(48000, 2, opus.Application(2049))
	if err != nil {
		return fmt.Errorf("error creating Opus encoder: %w", err)
	}

	pcmBuf := make([]int16, 960*2)
	opusBuf := make([]byte, 4000)

	errChan := make(chan error)

	go func() {
		for {
			select {
			case <-f.stopChan:
				f.cmd.Process.Kill()
				errChan <- nil
				return
			default:
				if err := binary.Read(stdout, binary.LittleEndian, &pcmBuf); err != nil {
					if err == io.EOF {
						log.Println("EOF reached, stopping playback")
						errChan <- nil
						return
					}
					errChan <- fmt.Errorf("error reading pcm data: %w", err)
					return
				}

				pcm := f.applyVolume(pcmBuf, f.currentVolume[vc.GuildID])

				n, err := opusEncoder.Encode(pcm, opusBuf)
				if err != nil {
					errChan <- fmt.Errorf("error encoding opus data: %w", err)
					return
				}

				vc.OpusSend <- append([]byte{}, opusBuf[:n]...)
			}
		}
	}()

	err = <-errChan
	time.Sleep(10 * time.Second)
	return err
}

func (f *FFmpegAdapter) applyVolume(pcm []int16, volume int) []int16 {
	if volume == 100 {
		return pcm // volume padrão, sem alteração
	}
	adjusted := make([]int16, len(pcm))
	vol := float64(volume) / 100.0

	for i, sample := range pcm {
		adjustedSample := float64(sample) * vol

		// Clamp para evitar overflow
		if adjustedSample > math.MaxInt16 {
			adjustedSample = math.MaxInt16
		} else if adjustedSample < math.MinInt16 {
			adjustedSample = math.MinInt16
		}
		adjusted[i] = int16(adjustedSample)
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
