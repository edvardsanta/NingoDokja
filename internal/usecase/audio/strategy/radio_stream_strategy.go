package strategy

import (
	"github.com/bwmarrin/discordgo"
	"read_books/internal/domain/audio"
)

type RadioStream struct {
	vc     *discordgo.VoiceConnection
	FFmpeg *domain.FFmpegAdapter
	Source string
}

func NewRadioStream(vc *discordgo.VoiceConnection, source string, encoder domain.AudioEncoder) *RadioStream {
	return &RadioStream{
		vc:     vc,
		FFmpeg: domain.NewFFmpegAdapter(encoder),
		Source: source,
	}
}

func (s *RadioStream) Play() error {
	err := s.FFmpeg.StreamAudio(s.Source)
	if err != nil {
		return err
	}

	go func() {
		for packet := range s.FFmpeg.EncodedPackets {
			if packet == nil || len(packet) == 0 {
				continue
			}
			s.vc.OpusSend <- packet
		}
	}()

	return nil
}

func (s *RadioStream) Stop() error {
	return s.FFmpeg.Stop()
}

func (s *RadioStream) SetVolume(volume int) error {
	return s.FFmpeg.SetVolume(volume)
}
