package audio

import (
	"encoding/binary"
	"log"
	"os"
	"read_books/internal/infrastructure/audio/decoder"
)

type AudioListener struct {
	stopChan       chan struct{}
	doneChan       chan struct{}
	filename       string
	pcmData        []int16
	encodedPackets <-chan []byte
}

func NewAudioListener(encodedPackets <-chan []byte) *AudioListener {
	return &AudioListener{
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		encodedPackets: encodedPackets,
	}
}

func (l *AudioListener) Start(filename string) {
	l.filename = filename
	go func() {
		opusDecoder, err := decoder.NewOpusDecoder(48000, 2) // 48kHz, 2 canais (estéreo)
		if err != nil {
			log.Printf("Erro ao criar decoder Opus: %v", err)
			close(l.doneChan)
			return
		}
		log.Println("Gravando áudio em PCM... (Ctrl+C para parar)")
	loop:
		for {
			select {
			case <-l.stopChan:
				break loop
			case opusData, ok := <-l.encodedPackets:
				if !ok {
					break loop
				}
				if err != nil {
					log.Printf("Erro ao ler pacote Opus: %v", err)
					break loop
				}

				if opusData == nil || len(opusData) == 0 {
					silence := make([]int16, 960*2)
					l.pcmData = append(l.pcmData, silence...)
					continue
				}

				pcm, err := opusDecoder.Decode(opusData, 5760)
				if err != nil {
					log.Printf("Erro ao decodificar Opus: %v", err)
					continue
				}
				l.pcmData = append(l.pcmData, pcm...)
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
