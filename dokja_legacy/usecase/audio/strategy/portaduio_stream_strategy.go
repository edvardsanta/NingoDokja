package strategy

import (
	"sync"

	domain "read_books/internal/dokja_legacy/domain/audio"

	"github.com/gordonklaus/portaudio"
)

type PortAudioStream struct {
	source   string
	FFmpeg   *domain.FFmpegAdapter
	stream   *portaudio.Stream
	stopChan chan struct{}
	stopped  bool
	mu       sync.Mutex
	volume   int
}

func (p *PortAudioStream) Play() error {
	err := portaudio.Initialize()
	if err != nil {
		return err
	}

	// Start ffmpeg to emit PCM to EncodedPackets
	err = p.FFmpeg.StreamAudio(nil, p.source)
	if err != nil {
		portaudio.Terminate()
		return err
	}

	outBuffer := make([]int16, 960*2) // 960 samples por canal, 2 canais (estéreo)

	// Parâmetros para stream de saída
	outputDevice, err := portaudio.DefaultOutputDevice()
	if err != nil {
		portaudio.Terminate()
		return err
	}

	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDevice,
			Channels: 2,
			Latency:  outputDevice.DefaultLowOutputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: len(outBuffer) / 2, // total frames = samples / canais
		Flags:           portaudio.ClipOff,
	}

	stream, err := portaudio.OpenStream(params, func(out []int16) {
		select {
		case <-p.stopChan:
			// preenche com silêncio e retorna
			for i := range out {
				out[i] = 0
			}
			return
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		portaudio.Terminate()
		return err
	}

	p.stream = stream

	err = p.stream.Start()
	if err != nil {
		stream.Close()
		portaudio.Terminate()
		return err
	}

	go func() {
		<-p.stopChan
		p.stream.Stop()
		p.stream.Close()
		portaudio.Terminate()
	}()

	return nil
}

func (p *PortAudioStream) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return nil
	}
	close(p.stopChan)
	p.stopped = true
	return p.FFmpeg.Stop()
}

func (p *PortAudioStream) SetVolume(volume int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = volume
	// Se quiser, pode implementar volume direto aqui
	return p.FFmpeg.SetVolume(nil, volume)
}
