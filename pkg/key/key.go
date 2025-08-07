package key

import (
	"github.com/ualinker/tdl/core/storage/keygen"
)

func App() string {
	return keygen.New("app")
}

func Resume(fingerprint string) string {
	return keygen.New("resume", fingerprint)
}
