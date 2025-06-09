package domain

type AudioSource interface {
	ReadPCMFrame() ([]int16, error)
	Close() error
}

type AudioEncoder interface {
	Encode(pcm []int16) ([]byte, error)
	FrameSamples() int
	SampleRate() int
	Channels() int
	Close() error
}

type AudioDecoder interface {
	Decode(data []byte, frames int, channels int) ([]int16, error)
	Close() error
}
