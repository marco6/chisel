package scripts_test

import (
	"strings"
	"testing"

	"github.com/canonical/chisel/internal/scripts"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/startest"
)

func isStarlarkCancellation(err error) bool {
	return strings.Contains(err.Error(), "Starlark computation cancelled:")
}

func TestContentValueSafeString(t *testing.T) {
	input := &scripts.ContentValue{}
	t.Run("nil-thread", func(t *testing.T) {
		builder := new(strings.Builder)
		if err := input.SafeString(nil, builder); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("consistency", func(t *testing.T) {
		thread := &starlark.Thread{}
		builder := new(strings.Builder)
		if err := input.SafeString(thread, builder); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		expected := input.String()
		actual := builder.String()
		if expected != actual {
			t.Errorf("inconsistent stringer implementation: expected %s got %s", expected, actual)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.TimeSafe)
		st.SetMaxSteps(0)
		st.RunThread(func(thread *starlark.Thread) {
			thread.Cancel("done")
			builder := starlark.NewSafeStringBuilder(thread)
			err := input.SafeString(thread, builder)
			if err == nil {
				st.Error("expected cancellation")
			} else if !isStarlarkCancellation(err) {
				st.Errorf("expected cancellation, got: %v", err)
			}
		})
	})
}

func TestContentValueSafeAttr(t *testing.T) {
	input := &scripts.ContentValue{}

	for _, attr := range input.AttrNames() {
		t.Run(attr, func(t *testing.T) {
			t.Run("nil-thread", func(t *testing.T) {
				_, err := input.SafeAttr(nil, attr)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			})

			t.Run("resources", func(t *testing.T) {
				st := startest.From(t)
				st.RequireSafety(starlark.CPUSafe | starlark.MemSafe)
				st.SetMaxSteps(0)
				st.RunThread(func(thread *starlark.Thread) {
					for i := 0; i < st.N; i++ {
						result, err := input.SafeAttr(thread, attr)
						if err != nil {
							st.Error(err)
						}
						st.KeepAlive(result)
					}
				})
			})

			t.Run("cancellation", func(t *testing.T) {
				st := startest.From(t)
				st.RequireSafety(starlark.TimeSafe)
				st.SetMaxSteps(0)
				st.RunThread(func(thread *starlark.Thread) {
					thread.Cancel("done")
					_, err := input.SafeAttr(thread, attr)
					if err != nil {
						st.Error(err)
					}
				})
			})
		})
	}
}
