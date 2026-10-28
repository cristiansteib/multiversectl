package app_test

import (
	"errors"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type fakeLoader map[string]*domain.Manifest

func (f fakeLoader) Decode([]byte, string) (*domain.Manifest, error) {
	return nil, errors.New("not decoded here")
}

func (f fakeLoader) Load(path string) (*domain.Manifest, error) {
	if m, ok := f[path]; ok {
		return m, nil
	}
	return nil, errors.New("no such manifest")
}
