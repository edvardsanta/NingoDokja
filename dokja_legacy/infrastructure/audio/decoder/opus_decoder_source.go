package decoder

import (
	"gopkg.in/hraban/opus.v2"
)

type OpusDecoder struct {
	decoder  *opus.Decoder
	channels int
}

func NewOpusDecoder(sampleRate int, channels int) (*OpusDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}
	return &OpusDecoder{decoder: dec, channels: channels}, nil
}

func (d *OpusDecoder) Decode(data []byte, frames int) ([]int16, error) {
	pcm := make([]int16, frames*d.channels)
	n, err := d.decoder.Decode(data, pcm)
	if err != nil {
		return nil, err
	}
	return pcm[:n*d.channels], nil
}

func (d *OpusDecoder) Close() error {
	// O decoder Opus não precisa de fechamento explícito, mas podemos limpar recursos se necessário
	return nil
}
