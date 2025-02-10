package scripts_test

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/canonical/chisel/internal/fsutil"
	"github.com/canonical/chisel/internal/scripts"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/startest"
	. "gopkg.in/check.v1"
)

func isStarlarkCancellation(err error) bool {
	return strings.Contains(err.Error(), "Starlark computation cancelled:")
}

func (s *S) TestContentSafeStringNilThread(c *C) {
	input := &scripts.ContentValue{}
	builder := new(strings.Builder)
	err := input.SafeString(nil, builder)
	c.Assert(err, IsNil)
}

func (s *S) TestContentSafeStringConsistency(c *C) {
	input := &scripts.ContentValue{}
	thread := &starlark.Thread{}
	builder := new(strings.Builder)
	err := input.SafeString(thread, builder)
	c.Assert(err, IsNil)
	expected := input.String()
	actual := builder.String()
	c.Assert(actual, Equals, expected)
}

func (s *S) TestContentSafeStringCancellation(c *C) {
	st := startest.From(c)
	st.RequireSafety(starlark.TimeSafe)
	st.SetMaxSteps(0)
	st.RunThread(func(thread *starlark.Thread) {
		thread.Cancel("done")
		builder := starlark.NewSafeStringBuilder(thread)
		input := &scripts.ContentValue{}
		err := input.SafeString(thread, builder)
		if err == nil {
			st.Error("expected cancellation")
		} else if !isStarlarkCancellation(err) {
			st.Errorf("expected cancellation, got: %v", err)
		}
	})
}

func (s *S) TestContentSafeAttrNilThread(c *C) {
	input := &scripts.ContentValue{}

	for _, attr := range input.AttrNames() {
		attr, err := input.SafeAttr(nil, attr)
		c.Assert(attr, NotNil)
		c.Assert(err, IsNil)
	}
}

func (s *S) TestContentSafeAttrResources(c *C) {
	input := &scripts.ContentValue{}
	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe | starlark.MemSafe)
	st.SetMaxSteps(0)
	for _, attr := range input.AttrNames() {
		st.RunThread(func(thread *starlark.Thread) {
			for i := 0; i < st.N; i++ {
				result, err := input.SafeAttr(thread, attr)
				if err != nil {
					st.Error(err)
				}
				st.KeepAlive(result)
			}
		})
	}
}

func (s *S) TestContentListSafetyAllocs(c *C) {
	baseDir := c.MkDir()

	err := os.Mkdir(baseDir+"/dir", fs.ModeDir|0765)
	c.Assert(err, IsNil)

	f, err := os.Create(baseDir + "/file")
	c.Assert(err, IsNil)
	f.Close()

	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	contentList, err := content.Attr("list")
	c.Assert(contentList, NotNil)
	c.Assert(err, IsNil)

	st := startest.From(c)
	st.RequireSafety(starlark.MemSafe)
	st.RunThread(func(thread *starlark.Thread) {
		path := starlark.String("/")
		for i := 0; i < st.N; i++ {
			result, err := starlark.Call(thread, contentList, starlark.Tuple{path}, nil)
			if err != nil {
				st.Error(err)
			}
			st.KeepAlive(result)
		}
	})
}

func (s *S) TestContentListSafetySteps(c *C) {
	baseDir := c.MkDir()

	err := os.Mkdir(baseDir+"/dir", fs.ModeDir|0765)
	c.Assert(err, IsNil)

	f, err := os.Create(baseDir + "/file")
	c.Assert(err, IsNil)
	f.Close()

	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	contentList, err := content.Attr("list")
	c.Assert(contentList, NotNil)
	c.Assert(err, IsNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMinSteps(2)
	st.SetMaxSteps(4)
	st.RunThread(func(thread *starlark.Thread) {
		path := starlark.String("/")
		for i := 0; i < st.N; i++ {
			_, err := starlark.Call(thread, contentList, starlark.Tuple{path}, nil)
			if err != nil {
				st.Error(err)
			}
		}
	})

}

func (s *S) TestContentListSafetyCancellation(c *C) {
	baseDir := c.MkDir()

	err := os.Mkdir(baseDir+"/dir", fs.ModeDir|0765)
	c.Assert(err, IsNil)

	f, err := os.Create(baseDir + "/file")
	c.Assert(err, IsNil)
	f.Close()

	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	contentList, err := content.Attr("list")
	c.Assert(contentList, NotNil)
	c.Assert(err, IsNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMaxSteps(0)
	st.RunThread(func(thread *starlark.Thread) {
		thread.Cancel("done")
		path := starlark.String("/")
		for i := 0; i < st.N; i++ {
			_, err := starlark.Call(thread, contentList, starlark.Tuple{path}, nil)
			if err == nil {
				st.Error("expected cancellation")
			} else if !isStarlarkCancellation(err) {
				st.Errorf("expected cancellation, got: %v", err)
			}
		}
	})
}

func (s *S) TestSafeReadFileCancellation(c *C) {
	thread := &starlark.Thread{}
	go func() {
		time.Sleep(500 * time.Millisecond)
		thread.Cancel("done")
	}()
	_, err := scripts.SafeReadFile(thread, "/dev/zero")
	c.Assert(err, NotNil)

	st := startest.From(c)
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
}

func (s *S) TestContentReadSafetyAllocs(c *C) {
	const path = "/file"
	const chunk = 1024

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	realPath, err := content.RealPath(path, scripts.CheckNone)
	c.Assert(err, IsNil)
	contentRead, err := content.Attr("read")
	c.Assert(err, IsNil)
	c.Assert(contentRead, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.MemSafe)
	st.RunThread(func(thread *starlark.Thread) {
		if err := writeNBytes(realPath, int64(st.N*chunk)); err != nil {
			st.Fatal(err)
		}
		defer func() {
			if err := os.Remove(realPath); err != nil {
				st.Fatal(err)
			}
		}()
		result, err := starlark.Call(thread, contentRead, starlark.Tuple{starlark.String(path)}, nil)
		if err != nil {
			st.Error(err)
		}
		st.KeepAlive(result)
	})
}

func (s *S) TestContentReadSafetySteps(c *C) {
	const path = "/file"
	const chunk = 1024

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	realPath, err := content.RealPath(path, scripts.CheckNone)
	c.Assert(err, IsNil)
	contentRead, err := content.Attr("read")
	c.Assert(err, IsNil)
	c.Assert(contentRead, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMinSteps(chunk)
	st.SetMaxSteps(chunk)
	st.RunThread(func(thread *starlark.Thread) {
		if err := writeNBytes(realPath, int64(st.N*chunk)); err != nil {
			st.Fatal(err)
		}
		defer func() {
			if err := os.Remove(realPath); err != nil {
				st.Fatal(err)
			}
		}()
		_, err = starlark.Call(thread, contentRead, starlark.Tuple{starlark.String(path)}, nil)
		if err != nil {
			st.Error(err)
		}
	})
}

func (s *S) TestContentReadSafetyCancellation(c *C) {
	const path = "/file"

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
	}
	contentRead, err := content.Attr("read")
	c.Assert(err, IsNil)
	c.Assert(contentRead, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMaxSteps(0)
	st.RunThread(func(thread *starlark.Thread) {
		thread.Cancel("done")
		for i := 0; i < st.N; i++ {
			_, err := starlark.Call(thread, contentRead, starlark.Tuple{starlark.String(path)}, nil)
			if err == nil {
				st.Error("expected cancellation")
			} else if err == context.Canceled {
				st.Errorf("expected cancellation, got: %v", err)
			}
		}
	})
}

func writeNBytes(path string, n int64) error {
	fd, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = fd.Seek(n-1, 0)
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

func (s *S) TestContentWriteSafetyAllocs(c *C) {
	const path = "/file"

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
		OnWrite: func(entry *fsutil.Entry) error {
			return nil
		},
	}
	contentWrite, err := content.Attr("write")
	c.Assert(err, IsNil)
	c.Assert(contentWrite, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.MemSafe)
	st.RunThread(func(thread *starlark.Thread) {
		result, err := starlark.Call(thread, contentWrite, starlark.Tuple{starlark.String(path), starlark.String(strings.Repeat("X", st.N))}, nil)
		if err != nil {
			st.Error(err)
		}
		st.KeepAlive(result)
	})
}

func (s *S) TestContentWriteSafetySteps(c *C) {
	const path = "/file"

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
		OnWrite: func(entry *fsutil.Entry) error {
			return nil
		},
	}
	contentWrite, err := content.Attr("write")
	c.Assert(err, IsNil)
	c.Assert(contentWrite, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMinSteps(1)
	st.SetMaxSteps(1)
	st.RunThread(func(thread *starlark.Thread) {
		_, err := starlark.Call(thread, contentWrite, starlark.Tuple{starlark.String(path), starlark.String(strings.Repeat("X", st.N))}, nil)
		if err != nil {
			st.Error(err)
		}
	})
}

func (s *S) TestContentWriteSafetyCancellation(c *C) {
	const path = "/file"

	baseDir := c.MkDir()
	content := scripts.ContentValue{
		RootDir: baseDir,
		OnWrite: func(entry *fsutil.Entry) error {
			return nil
		},
	}
	contentWrite, err := content.Attr("write")
	c.Assert(err, IsNil)
	c.Assert(contentWrite, NotNil)

	st := startest.From(c)
	st.RequireSafety(starlark.CPUSafe)
	st.SetMaxSteps(0)
	st.RunThread(func(thread *starlark.Thread) {
		thread.Cancel("done")
		for i := 0; i < st.N; i++ {
			_, err := starlark.Call(thread, contentWrite, starlark.Tuple{starlark.String(path), starlark.String("x")}, nil)
			if err == nil {
				st.Error("expected cancellation")
			} else if err == context.Canceled {
				st.Errorf("expected cancellation, got: %v", err)
			}
		}
	})
}
