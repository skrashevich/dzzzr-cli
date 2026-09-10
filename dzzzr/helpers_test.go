package dzzzr_test

import (
	"errors"
	"fmt"
)

func asErr[T any](err error, target *T) bool { return errors.As(err, target) }

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
