package mic

import (
	"github.com/gordonklaus/portaudio"
)

type MicrophoneSource struct {
	stream *portaudio.Stream
	buffer []int16
}

func NewMicrophoneSource() (*MicrophoneSource, error) {
	err := portaudio.Initialize()
	if err != nil {
		return nil, err
	}
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}

	for i, dev := range devices {
		println(i, dev.Name, dev.MaxInputChannels, dev.HostApi.Name)
	}

	buffer := make([]int16, 960) // 960 samples per channel, 2 channels
	inputDevice := devices[16]

	params := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDevice,
			Channels: 1, // mono, como indicado
			Latency:  inputDevice.DefaultLowInputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: len(buffer), // len(buffer) deve ser múltiplo de canais
		Flags:           portaudio.ClipOff,
	}

	stream, err := portaudio.OpenStream(params, &buffer)

	if err != nil {
		portaudio.Terminate()
		return nil, err
	}

	err = stream.Start()
	if err != nil {
		stream.Close()
		portaudio.Terminate()
		return nil, err
	}

	return &MicrophoneSource{
		stream: stream,
		buffer: buffer,
	}, nil
}

func (m *MicrophoneSource) ReadPCMFrame() ([]int16, error) {
	err := m.stream.Read()
	if err != nil {
		return nil, err
	}

	// Converte mono → estéreo duplicado
	mono := m.buffer
	return mono, nil
}

func (m *MicrophoneSource) Close() error {
	m.stream.Stop()
	m.stream.Close()
	portaudio.Terminate()
	return nil
}
