package utils

import (
	"path/filepath"
	"runtime"
)

func GetUsecaseFromPath() string {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		panic("Could not get caller information")
	}
	dir := filepath.Base(filepath.Dir(filename))
	return dir
}
