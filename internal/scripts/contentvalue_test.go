package scripts_test

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/canonical/chisel/internal/scripts"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/startest"
)

func isStarlarkCancellation(err error) bool {
	return strings.Contains(err.Error(), "Starlark computation cancelled:")
}

func TestContentSafeString(t *testing.T) {
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

func TestContentSafeAttr(t *testing.T) {
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

func TestContentListSafety(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.Mkdir(baseDir+"/dir", fs.ModeDir|0765); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Create(baseDir + "/file"); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}

	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	content_list, _ := content.Attr("list")
	if content_list == nil {
		t.Fatal("no such method: Content.list")
	}

	t.Run("allocs", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.MemSafe)
		st.RunThread(func(thread *starlark.Thread) {
			path := starlark.String("/")
			for i := 0; i < st.N; i++ {
				result, err := starlark.Call(thread, content_list, starlark.Tuple{path}, nil)
				if err != nil {
					st.Error(err)
				}
				st.KeepAlive(result)
			}
		})
	})

	t.Run("steps", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.CPUSafe)
		st.SetMinSteps(2)
		st.SetMaxSteps(4)
		st.RunThread(func(thread *starlark.Thread) {
			path := starlark.String("/")
			for i := 0; i < st.N; i++ {
				_, err := starlark.Call(thread, content_list, starlark.Tuple{path}, nil)
				if err != nil {
					st.Error(err)
				}
			}
		})
	})

	t.Run("cancellation", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.CPUSafe)
		st.SetMaxSteps(0)
		st.RunThread(func(thread *starlark.Thread) {
			thread.Cancel("done")
			path := starlark.String("/")
			for i := 0; i < st.N; i++ {
				_, err := starlark.Call(thread, content_list, starlark.Tuple{path}, nil)
				if err == nil {
					st.Error("expected cancellation")
				} else if !isStarlarkCancellation(err) {
					st.Errorf("expected cancellation, got: %v", err)
				}
			}
		})
	})
}

func TestSafeReadFileCancellation(t *testing.T) {
	t.Run("already-cancelled", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.CPUSafe)
		st.SetMaxSteps(0)
		st.RunThread(func(thread *starlark.Thread) {
			thread.Cancel("done")
			for i := 0; i < st.N; i++ {
				_, err := scripts.SafeReadFile(thread, "/dev/zero")
				if err == nil {
					st.Error("expected cancellation")
				} else if err != context.Canceled {
					st.Errorf("expected cancellation, got: %v", err)
				}
			}
		})
	})

	t.Run("eventually-cancelled", func(t *testing.T) {
		thread := &starlark.Thread{}
		go func() {
			time.Sleep(50 * time.Millisecond)
			thread.Cancel("done")
		}()
		_, err := scripts.SafeReadFile(thread, "/dev/zero")
		if err == nil {
			t.Error("expected cancellation")
		} else if err != context.Canceled {
			t.Errorf("expected cancellation, got: %v", err)
		}
	})
}

func TestContentReadSafety(t *testing.T) {
	const path = "/file"
	const chunk = 1024

	baseDir := t.TempDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	content_read, _ := content.Attr("read")
	if content_read == nil {
		t.Fatal("no such method: Content.read")
	}

	writeZeroFile := func(path string, size int64) error {
		fd, err := os.Create(path)
		if err != nil {
			return err
		}
		_, err = fd.Seek(size-1, 0)
		if err != nil {
			return err
		}
		_, err = fd.Write([]byte{0})
		if err != nil {
			return err
		}
		err = fd.Close()
		if err != nil {
			return err
		}
		return nil
	}

	t.Run("allocs", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.MemSafe)
		st.RunThread(func(thread *starlark.Thread) {
			realPath, err := content.RealPath(path, scripts.CheckNone)
			if err != nil {
				st.Fatal(err)
			}
			if err := writeZeroFile(realPath, int64(st.N*chunk)); err != nil {
				st.Fatal(err)
			}
			defer func() {
				if err := os.Remove(realPath); err != nil {
					st.Fatal(err)
				}
			}()
			result, err := starlark.Call(thread, content_read, starlark.Tuple{starlark.String(path)}, nil)
			if err != nil {
				st.Error(err)
			}
			st.KeepAlive(result)
		})
	})

	t.Run("steps", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.CPUSafe)
		st.SetMinSteps(chunk)
		st.SetMaxSteps(chunk)
		st.RunThread(func(thread *starlark.Thread) {
			realPath, err := content.RealPath(path, scripts.CheckNone)
			if err != nil {
				st.Fatal(err)
			}
			if err := writeZeroFile(realPath, int64(st.N*chunk)); err != nil {
				st.Fatal(err)
			}
			defer func() {
				if err := os.Remove(realPath); err != nil {
					st.Fatal(err)
				}
			}()
			_, err = starlark.Call(thread, content_read, starlark.Tuple{starlark.String(path)}, nil)
			if err != nil {
				st.Error(err)
			}
		})
	})

	t.Run("cancellation", func(t *testing.T) {
		st := startest.From(t)
		st.RequireSafety(starlark.CPUSafe)
		st.SetMaxSteps(0)
		st.RunThread(func(thread *starlark.Thread) {
			thread.Cancel("done")
			path := starlark.String("/file")
			for i := 0; i < st.N; i++ {
				_, err := starlark.Call(thread, content_read, starlark.Tuple{path}, nil)
				if err == nil {
					st.Error("expected cancellation")
				} else if err == context.Canceled {
					st.Errorf("expected cancellation, got: %v", err)
				}
			}
		})
	})
}
