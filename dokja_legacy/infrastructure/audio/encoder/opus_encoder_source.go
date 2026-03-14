package encoder

import "gopkg.in/hraban/opus.v2"

type OpusEncoder struct {
	encoder           *opus.Encoder
	maxOpusPacketSize int
	frameSize         int
	channels          int
	sampleRate        int
}

func NewOpusEncoder(sampleRate, frameSize, channels, maxOpusPacketSize int, app opus.Application) (*OpusEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, app)
	if err != nil {
		return nil, err
	}
	return &OpusEncoder{
		encoder:           enc,
		maxOpusPacketSize: maxOpusPacketSize,
		frameSize:         frameSize,
		channels:          channels,
	}, nil
}

func (e *OpusEncoder) FrameSamples() int {
	// O Opus usa 960 samples por quadro para 48kHz, que é o padrão
	return e.frameSize * e.channels
}

func (e *OpusEncoder) SampleRate() int {
	// Retorna a taxa de amostragem do encoder Opus
	return e.sampleRate
}

func (e *OpusEncoder) Channels() int {
	// Retorna o número de canais do encoder Opus
	return e.channels
}

func (e *OpusEncoder) Encode(pcm []int16) ([]byte, error) {
	buf := make([]byte, e.maxOpusPacketSize)
	n, err := e.encoder.Encode(pcm, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (e *OpusEncoder) Close() error {
	// O encoder Opus não precisa de fechamento explícito, mas podemos limpar recursos se necessário
	return nil
}
