package wav

import (
	"encoding/binary"
	"fmt"
	"os"
)

func SaveWAV(filename string, pcmData []int16, sampleRate uint32, numChannels uint16) error {
	file, err := os.Create(filename + ".wav")
	if err != nil {
		return fmt.Errorf("erro criando arquivo: %w", err)
	}
	defer file.Close()

	numSamples := uint32(len(pcmData))
	byteRate := sampleRate * uint32(numChannels) * 2 // 16-bit = 2 bytes
	blockAlign := numChannels * 2
	dataLen := numSamples * 2

	// RIFF Header
	if _, err := file.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, uint32(36+dataLen)); err != nil {
		return err
	}
	if _, err := file.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, uint32(16)); err != nil { // Subchunk1Size
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, uint16(1)); err != nil { // PCM
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, numChannels); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, sampleRate); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, uint16(16)); err != nil { // BitsPerSample
		return err
	}
	if _, err := file.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, dataLen); err != nil {
		return err
	}

	binary.Write(file, binary.LittleEndian, pcmData)

	return nil
}
