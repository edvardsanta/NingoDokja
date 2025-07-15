package main

import (
	"encoding/json"
	"fmt"
	vosk "github.com/alphacep/vosk-api/go"
	"io"
	"log"
	"os"
	"os/exec"
	wav "read_books/internal/infrastructure/audio"
	"read_books/internal/infrastructure/audio/decoder"
	"read_books/internal/infrastructure/audio/encoder"
	"read_books/internal/infrastructure/audio/mic"
	"read_books/internal/usecase/audio"
	"strings"
	"time"
)

type PartialResult struct {
	Partial string `json:"partial"`
}

type FinalResult struct {
	Text string `json:"text"`
}

func printIfValid(result string) {
	var pr PartialResult
	var fr FinalResult

	if strings.Contains(result, `"partial"`) {
		if err := json.Unmarshal([]byte(result), &pr); err == nil {
			if pr.Partial != "" {
				fmt.Println(pr)
			}
		}
	} else if strings.Contains(result, `"text"`) {
		if err := json.Unmarshal([]byte(result), &fr); err == nil {
			if fr.Text != "" && fr.Text != "<UNK>" {
				fmt.Println(fr)
			}
		}
	}
}

func test() {
	micSource, err := mic.NewMicrophoneSource()
	if err != nil {
		log.Fatalf("Erro iniciando microfone: %v", err)
	}

	var allPCM []int16
	log.Println("Gravando áudio do microfone por 5 segundos...")

	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case <-timeout:
			break loop
		default:
			pcmFrame, err := micSource.ReadPCMFrame()
			if err != nil {
				log.Printf("Erro lendo frame do microfone: %v", err)
				continue
			}
			allPCM = append(allPCM, pcmFrame...)
		}
	}

	log.Println("Gravação finalizada, salvando em arquivo...")

	// Parâmetros comuns de microfone: 48000Hz, mono
	err = wav.SaveWAV("saida_mic.wav", allPCM, 48000, 1)
	if err != nil {
		log.Fatalf("Erro salvando WAV: %v", err)
	}

	log.Println("Arquivo 'saida_mic.wav' salvo com sucesso.")
}

func runShellCommand(cmd string) error {
	c := exec.Command("bash", "-c", cmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func main() {
	micSource, err := mic.NewMicrophoneSource()
	if err != nil {
		log.Fatalf("Erro iniciando microfone: %v", err)
	}
	opusEncoder := encoder.NewPCMEncoderSource() // maxPacketSize ~4000, frameSize=960
	//opusEncoder, err := encoder.NewOpusEncoder(48000, 960, 1, 4000, opus.AppAudio) // maxPacketSize ~4000, frameSize=960
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
	//var opusDecoder, _ = decoder.NewOpusDecoder(48000, 1) // maxPacketSize ~4000, frameSize=960
	var opusDecoder = decoder.NewPcmDecoder(1) // maxPacketSize ~4000, frameSize=960
	audioListener := audio.NewAudioListener(encodedPackets, opusDecoder)
	audioListener.Start() // Start listening immediately
	log.Println("Gravando áudio por 10 segundos...")
	time.Sleep(10 * time.Second) // Record for 10 seconds

	pcmSlice, err := audioListener.Stop()
	if err != nil {
		return
	}
	err = wav.SaveWAV("gravacao", pcmSlice, 48000, 1) // sampleRate=48000, numChannels=1
	if err != nil {
		log.Fatalf("Erro salvando WAV: %v", err)
	}
	log.Println("Gravação finalizada. Verifique o arquivo 'gravacao.wav'.")

	cmd := `ffmpeg -y -i gravacao.wav -af "afftdn=nf=-25" -ar 16000 -ac 1 -sample_fmt s16 gravacao_vosk.wav`
	log.Println("Convertendo para arquivo compatível com Vosk usando ffmpeg...")
	if err := runShellCommand(cmd); err != nil {
		log.Fatalf("Erro ao converter com ffmpeg: %v", err)
	}
	log.Println("Arquivo para Vosk salvo como 'gravacao_vosk.wav'.")

	// Vosk code is commented out for now
	model, err := vosk.NewModel("")
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}
	defer model.Free()

	rec, err := vosk.NewRecognizer(model, 16000.0) // sample rate must match your audio
	if err != nil {
		log.Fatalf("Failed to create recognizer: %v", err)
	}
	defer rec.Free()

	f, err := os.Open("gravacao_vosk.wav")
	if err != nil {
		log.Fatalf("Failed to open audio file: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 4000)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			var res string
			if rec.AcceptWaveform(buf[:n]) != 0 {
				res = rec.Result()
			} else {
				res = rec.PartialResult()
			}
			printIfValid(res)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Error reading audio: %v", err)
		}
	}
	fmt.Println(rec.FinalResult())
}
