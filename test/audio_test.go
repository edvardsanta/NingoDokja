package test

import (
	"gopkg.in/hraban/opus.v2"
	"log"
	"read_books/internal/infrastructure/audio/encoder"
	domain "read_books/internal/infrastructure/audio/mic"
	"read_books/internal/usecase/audio"
	"read_books/internal/usecase/audio/strategy"
	"testing"
	"time"
)

func TestAudioListenerUseCase(t *testing.T) {
	micSource, err := domain.NewMicrophoneSource()
	if err != nil {
		log.Fatalf("Erro iniciando microfone: %v", err)
	}
	opusEncoder, err := encoder.NewOpusEncoder(48000, 960, 2, 4000, 2048) // maxPacketSize ~4000, frameSize=960
	if err != nil {
		log.Fatalf("Erro criando encoder Opus: %v", err)
	}
	encodedPackets := make(chan []byte, 100)
	go func() {
		for {
			pcmFrame, err := micSource.ReadPCMFrame()
			if err != nil {
				log.Printf("Erro lendo frame do microfone: %v", err)
				continue
			}
			encoded, err := opusEncoder.Encode(pcmFrame)
			if err != nil {
				log.Printf("Erro codificando PCM: %v", err)
				continue
			}
			encodedPackets <- encoded
		}
	}()
	audioListener := audio.NewAudioListener(encodedPackets)
	time.Sleep(1 * time.Second)
	audioListener.Start("gravacao")
	time.Sleep(5 * time.Second)
}

func TestPortAudioStream_Play_Stop(t *testing.T) {
	source := ""

	opusEncoder, _ := encoder.NewOpusEncoder(48000, 960, 2, 4000, opus.AppAudio)
	player, _ := strategy.NewPortAudioStream(source, opusEncoder)

	go func() {
		err := player.Play()
		if err != nil {
			t.Errorf("Erro no Play: %v", err)
		}
	}()

	time.Sleep(19 * time.Second)

	err := player.Stop()
	if err != nil {
		t.Errorf("Erro ao parar: %v", err)
	}

	time.Sleep(10 * time.Second)

}
