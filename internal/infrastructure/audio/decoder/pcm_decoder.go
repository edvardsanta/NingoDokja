package decoder

import (
	"bytes"
	"encoding/binary"
	"errors"
)

type PcmDecoder struct {
	channels int
}

func NewPcmDecoder(channels int) *PcmDecoder {
	return &PcmDecoder{
		channels: channels,
	}
}

func (d *PcmDecoder) Decode(data []byte, frames int) ([]int16, error) {
	requiredBytes := frames * d.channels * 2 // 2 bytes per sample 16-bit
	if len(data) < requiredBytes {
		return nil, errors.New("dados insuficientes para número de frames e canais solicitados")
	}
	samples := make([]int16, frames*d.channels)
	err := binary.Read(
		bytes.NewReader(data[:requiredBytes]),
		binary.LittleEndian,
		&samples,
	)
	if err != nil {
		return nil, err
	}
	return samples, nil
}

func (d *PcmDecoder) Close() error {
	return nil
}
