package strategy

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"os"
	"path/filepath"
	"read_books/internal/domain/audio"
)

type LocalFolderStrategy struct {
	FFmpeg *domain.FFmpegAdapter
	Source string
}

func (l *LocalFolderStrategy) Play(vc *discordgo.VoiceConnection) error {
	// Lógica para tocar todos os arquivos da pasta usando FFmpegAdapter
	// Exemplo simplificado:
	// files := ... // buscar arquivos em l.SourceFolder
	// para cada arquivo: l.FFmpeg.StreamAudio(vc, arquivo)
	return nil
}

func (l *LocalFolderStrategy) Stop() error {
	return l.FFmpeg.Stop()
}

func (l *LocalFolderStrategy) SetVolume(_ *discordgo.VoiceConnection, volume int) error {
	return l.FFmpeg.SetVolume(volume)
}

func (l *LocalFolderStrategy) findSongFolder() (string, error) {
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
