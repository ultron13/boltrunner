package store

import (
	"context"
	"errors"

	"github.com/boltrunner/backend/internal/model"
)

var ErrNotFound = errors.New("not found")

type TestStore interface {
	CreateTest(ctx context.Context, t *model.Test) error
	ListTests(ctx context.Context) ([]model.Test, error)
	GetTest(ctx context.Context, id string) (*model.Test, error)
}
