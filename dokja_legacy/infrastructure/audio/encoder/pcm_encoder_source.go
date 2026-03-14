package encoder

import (
	"bytes"
	"encoding/binary"
)

type PCMEncoderSource struct {
}

func NewPCMEncoderSource() *PCMEncoderSource {
	return &PCMEncoderSource{}
}

func (P PCMEncoderSource) Encode(pcm []int16) ([]byte, error) {
	var buf bytes.Buffer
	err := binary.Write(&buf, binary.LittleEndian, pcm)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (P PCMEncoderSource) FrameSamples() int {
	//TODO implement me
	panic("implement me")
}

func (P PCMEncoderSource) SampleRate() int {
	//TODO implement me
	panic("implement me")
}

func (P PCMEncoderSource) Channels() int {
	//TODO implement me
	panic("implement me")
}

func (P PCMEncoderSource) Close() error {
	//TODO implement me
	panic("implement me")
}
