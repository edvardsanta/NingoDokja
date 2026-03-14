package domain

import (
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSetVolume(t *testing.T) {
	adapter := NewFFmpegAdapter()

	testCases := []struct {
		name        string
		vc          *discordgo.VoiceConnection
		volume      int
		expectError bool
		expectedVol int
	}{
		{
			name:        "Valid volume",
			vc:          &discordgo.VoiceConnection{GuildID: "1234"},
			volume:      75,
			expectError: false,
			expectedVol: 75,
		},
		{
			name:        "Nil voice connection",
			vc:          nil,
			volume:      75,
			expectError: true,
		},
		{
			name:        "Volume below range",
			vc:          &discordgo.VoiceConnection{GuildID: "1234"},
			volume:      -10,
			expectError: true,
		},
		{
			name:        "Volume above range",
			vc:          &discordgo.VoiceConnection{GuildID: "1234"},
			volume:      150,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := adapter.SetVolume(tc.vc, tc.volume)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedVol, adapter.currentVolume[tc.vc.GuildID])
			}
		})
	}
}
