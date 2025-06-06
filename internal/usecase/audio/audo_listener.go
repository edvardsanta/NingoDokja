package audio

import (
	"bytes"
	"encoding/binary"
	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
	"log"
	"os"
)

type AudioListener struct {
	stopChan chan struct{}
	doneChan chan struct{}
	buffer   bytes.Buffer
	filename string
	pcmData  []int16
}

func NewAudioListener() *AudioListener {
	return &AudioListener{
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (l *AudioListener) Start(vc *discordgo.VoiceConnection, filename string) {
	l.filename = filename
	go func() {
		decoder, err := opus.NewDecoder(48000, 2)
		if err != nil {
			log.Printf("Erro ao criar decoder Opus: %v", err)
			close(l.doneChan)
			return
		}
		log.Println("Gravando áudio em PCM... (Ctrl+C para parar)")
		if vc == nil || vc.OpusRecv == nil {
			log.Println("Conexão de voz inválida.")
			close(l.doneChan)
			return
		}
	loop:
		for {
			select {
			case <-l.stopChan:
				break loop
			case pkt, ok := <-vc.OpusRecv:
				if !ok {
					log.Println("Canal de áudio fechado ou conexão de voz inválida.")
					break loop
				}
				if pkt == nil || len(pkt.Opus) == 0 {
					silence := make([]int16, 960*2)
					l.pcmData = append(l.pcmData, silence...)
					continue
				}
				pcm := make([]int16, 960*2)
				for i := range pcm {
					pcm[i] = 0
				}
				n, err := decoder.Decode(pkt.Opus, pcm)
				if err != nil {
					log.Printf("Erro ao decodificar Opus: %v", err)
					continue
				}
				l.pcmData = append(l.pcmData, pcm[:n*2]...)
			}
		}
		close(l.doneChan)
	}()
}

func (l *AudioListener) Stop() {
	close(l.stopChan)
	<-l.doneChan

	/*
		TODO: This block logic should be moved to a separate function
		as it is not related to the audio listener itself.
		Sincerely, I don't know if i will save as a wav file or as a raw pcm file.
	*/
	wavFile := l.filename + ".wav"
	file, err := os.Create(wavFile)
	if err != nil {
		log.Printf("Erro ao criar arquivo wav: %v", err)
		return
	}
	defer file.Close()

	// Writing some wav header information
	numSamples := uint32(len(l.pcmData))
	byteRate := uint32(48000 * 2 * 2)
	blockAlign := uint16(2 * 2)
	dataLen := numSamples * 2

	// RIFF header
	file.Write([]byte("RIFF"))
	binary.Write(file, binary.LittleEndian, uint32(36+dataLen))
	file.Write([]byte("WAVEfmt "))
	binary.Write(file, binary.LittleEndian, uint32(16))    // Subchunk1Size
	binary.Write(file, binary.LittleEndian, uint16(1))     // AudioFormat (PCM)
	binary.Write(file, binary.LittleEndian, uint16(2))     // NumChannels
	binary.Write(file, binary.LittleEndian, uint32(48000)) // SampleRate
	binary.Write(file, binary.LittleEndian, byteRate)      // ByteRate
	binary.Write(file, binary.LittleEndian, blockAlign)    // BlockAlign
	binary.Write(file, binary.LittleEndian, uint16(16))    // BitsPerSample
	file.Write([]byte("data"))
	binary.Write(file, binary.LittleEndian, dataLen)

	binary.Write(file, binary.LittleEndian, l.pcmData)

	log.Printf("Arquivo WAV salvo: %s", wavFile)
	l.pcmData = nil
	l.doneChan = make(chan struct{})
	l.stopChan = make(chan struct{})
}

func (l *AudioListener) IsRecording() bool {
	select {
	case <-l.doneChan:
		return false
	default:
		return true
	}
}
