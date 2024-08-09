package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"read_books/internal/logger"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

const ffmpegURL = "https://24293.live.streamtheworld.com/RADIO_KISSFM_ADP.aac"

var (
	stopChan  chan struct{}
	stopMutex sync.Mutex
)

func init() {
	stopChan = make(chan struct{})
}

func PlayAllSounds(vc *discordgo.VoiceConnection) error {
	stopMutex.Lock()
	stopChan = make(chan struct{})
	stopMutex.Unlock()

	for {
		select {
		case <-stopChan:
			logger.Info("Parando musicas")
			return nil
		default:
			pwd, err := findSongFolder()
			if err != nil {
				logger.Error("Ocorreu um erro ao encontrar a pasta de musicas", err)
				return err
			}
			files, err := os.ReadDir(pwd)
			if err != nil {
				log.Printf("Erro ao ler diretorio: %v", err)
				return err
			}

			for _, file := range files {
				select {
				case <-stopChan:
					log.Println("Playback stopped")
					return nil
				default:
					if file.Type().IsRegular() {
						filePath := filepath.Join(pwd, file.Name())
						if _, err := os.Stat(filePath); os.IsNotExist(err) {
							log.Printf("Arquivo não existe: %s", filePath)
							continue
						}

						log.Printf("Tocando: %s", filePath)
						if err := playStream(vc, filePath, false); err != nil {
							log.Printf("Erro ao tocar som %v", err)
						}
					}
				}
			}
			log.Printf("Todos sons tocados, reiniciando...")
		}
	}
}

func findSongFolder() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("error getting current working directory: %w", err)
	}

	for {
		songDir := filepath.Join(dir, "song")
		if stat, err := os.Stat(songDir); err == nil && stat.IsDir() {
			return songDir, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", fmt.Errorf("song folder not found")
}
func resetStopChan() {
	stopMutex.Lock()
	defer stopMutex.Unlock()
	if stopChan != nil {
		close(stopChan)
	}
	stopChan = make(chan struct{})
}
func PlayRadioStream(vc *discordgo.VoiceConnection, radioURL string) error {
	resetStopChan()

	stopMutex.Lock()
	stopChan = make(chan struct{})
	stopMutex.Unlock()

	log.Printf("Playing radio stream: %s", radioURL)
	return playStream(vc, radioURL, true)
}

func playStream(vc *discordgo.VoiceConnection, source string, isURL bool) error {
	var ffmpegCmd *exec.Cmd
	if isURL {
		ffmpegCmd = exec.Command("ffmpeg", "-i", source, "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:1")
	} else {
		ffmpegCmd = exec.Command("ffmpeg", "-i", source, "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:1")
	}

	ffmpegOut, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error getting ffmpeg stdout: %w", err)
	}

	if err := ffmpegCmd.Start(); err != nil {
		return fmt.Errorf("error starting ffmpeg command: %w", err)
	}

	vc.Speaking(true)
	defer vc.Speaking(false)

	opusEncoder, err := opus.NewEncoder(48000, 2, opus.Application(2049))
	if err != nil {
		return fmt.Errorf("error creating Opus encoder: %w", err)
	}

	pcmBuf := make([]int16, 960*2)
	opusBuf := make([]byte, 4000)

	errChan := make(chan error)

	go func() {
		for {
			select {
			case <-stopChan:
				ffmpegCmd.Process.Kill()
				errChan <- nil
				return
			default:
				if err := binary.Read(ffmpegOut, binary.LittleEndian, &pcmBuf); err != nil {
					if err == io.EOF {
						log.Println("EOF reached, stopping playback")
						errChan <- nil
						return
					}
					errChan <- fmt.Errorf("error reading pcm data: %w", err)
					return
				}

				n, err := opusEncoder.Encode(pcmBuf, opusBuf)
				if err != nil {
					errChan <- fmt.Errorf("error encoding opus data: %w", err)
					return
				}

				vc.OpusSend <- append([]byte{}, opusBuf[:n]...)
			}
		}
	}()

	err = <-errChan
	time.Sleep(10 * time.Second)
	return err
}

func StopPlaying() {
	stopMutex.Lock()
	defer stopMutex.Unlock()

	if stopChan != nil {
		close(stopChan)
	}
}
