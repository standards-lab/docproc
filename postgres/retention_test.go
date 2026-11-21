package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/postgres"
)

func TestNewRetentionRefusesItsArguments(t *testing.T) {
	func() {
		defer func() {
			if p := recover(); p != "postgres: NewRetention's db is nil" {
				t.Errorf("NewRetention with a nil db: panic %v", p)
			}
		}()
		_, _ = postgres.NewRetention(nil, time.Hour)
	}()
	for _, age := range []time.Duration{0, -time.Hour} {
		if r, err := postgres.NewRetention(fake(t), age); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("NewRetention with age %s = %v, %v, want ErrRequest", age, r, err)
		}
	}
	if _, err := postgres.NewRetention(fake(t), time.Nanosecond); err != nil {
		t.Errorf("NewRetention with a positive age: %v", err)
	}
}

func TestPassRefusesABudgetBelowOne(t *testing.T) {
	r, err := postgres.NewRetention(fake(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int{0, -1} {
		if more, err := r.Pass(t.Context(), budget); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("pass with budget %d = %v, %v, want ErrRequest", budget, more, err)
		}
	}
}
