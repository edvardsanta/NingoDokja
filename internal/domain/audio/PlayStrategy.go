package domain

type PlayStrategy interface {
	Play() error
	Stop() error
	SetVolume(volume int) error
}
