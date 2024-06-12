package scripts

import (
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/syntax"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Value = starlark.Value

type RunOptions struct {
	Label     string
	Namespace map[string]Value
	Script    string
}

const requiredSafety = starlark.CPUSafe | starlark.MemSafe | starlark.TimeSafe

var dialect = &syntax.FileOptions{
	Set:               false,
	While:             false,
	TopLevelControl:   false,
	GlobalReassign:    true,
	LoadBindsGlobally: false,
	Recursion:         false,
}

func Run(opts *RunOptions) error {
	thread := &starlark.Thread{Name: opts.Label}
	thread.RequireSafety(requiredSafety)
	_, err := starlark.ExecFileOptions(dialect, thread, opts.Label, opts.Script, opts.Namespace)
	return err
}

type ContentValue struct {
	RootDir    string
	CheckRead  func(path string) error
	CheckWrite func(path string) error
}

// Content starlark.Value interface
// --------------------------------------------------------------------------

var _ Value = &ContentValue{}

func (c *ContentValue) String() string {
	return "Content{...}"
}

func (c *ContentValue) Type() string {
	return "Content"
}

func (c *ContentValue) Freeze() {
}

func (c *ContentValue) Truth() starlark.Bool {
	return true
}

func (c *ContentValue) Hash() (uint32, error) {
	return starlark.String(c.RootDir).Hash()
}

// Content starlark.SafeStringer interface
// --------------------------------------------------------------------------

var _ starlark.SafeStringer = &ContentValue{}

func (c *ContentValue) SafeString(thread *starlark.Thread, sb starlark.StringBuilder) error {
	_, err := sb.WriteString("Content{...}")
	return err
}

// Content starlark.HasSafeAttrs interface
// --------------------------------------------------------------------------

var _ starlark.HasSafeAttrs = &ContentValue{}

func (c *ContentValue) Attr(name string) (Value, error) { return c.SafeAttr(nil, name) }

func (c *ContentValue) SafeAttr(thread *starlark.Thread, name string) (Value, error) {
	const safety = starlark.CPUSafe | starlark.MemSafe | starlark.TimeSafe | starlark.IOSafe
	if err := starlark.CheckSafety(thread, safety); err != nil {
		return nil, err
	}
	method, ok := contentValueMethods[name]
	if !ok {
		return nil, starlark.ErrNoSuchAttr
	}
	if thread != nil {
		if err := thread.AddAllocs(starlark.EstimateSize(&starlark.Builtin{})); err != nil {
			return nil, err
		}
	}
	return method.BindReceiver(c), nil
}

func (c *ContentValue) AttrNames() []string {
	return []string{"read", "write", "list"}
}

// Content methods
// --------------------------------------------------------------------------

type Check uint

const (
	CheckNone = 0
	CheckRead = 1 << iota
	CheckWrite
)

var contentValueMethods = map[string]*starlark.Builtin{
	"read":  starlark.NewBuiltinWithSafety("read", starlark.NotSafe, contentValueRead),
	"write": starlark.NewBuiltinWithSafety("write", starlark.NotSafe, contentValueWrite),
	"list":  starlark.NewBuiltinWithSafety("list", starlark.NotSafe, contentValueList),
}

func (c *ContentValue) RealPath(path string, what Check) (string, error) {
	if !filepath.IsAbs(c.RootDir) {
		return "", fmt.Errorf("internal error: content defined with relative root: %s", c.RootDir)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("content path must be absolute, got: %s", path)
	}
	cpath := filepath.Clean(path)
	if cpath != "/" && strings.HasSuffix(path, "/") {
		cpath += "/"
	}
	if c.CheckRead != nil && what&CheckRead != 0 {
		err := c.CheckRead(cpath)
		if err != nil {
			return "", err
		}
	}
	if c.CheckWrite != nil && what&CheckWrite != 0 {
		err := c.CheckWrite(cpath)
		if err != nil {
			return "", err
		}
	}
	rpath := filepath.Join(c.RootDir, path)
	if !filepath.IsAbs(rpath) || rpath != c.RootDir && !strings.HasPrefix(rpath, c.RootDir+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid content path: %s", path)
	}
	if lname, err := os.Readlink(rpath); err == nil {
		lpath := filepath.Join(filepath.Dir(rpath), lname)
		lrel, err := filepath.Rel(c.RootDir, lpath)
		if err != nil || !filepath.IsAbs(lpath) || lpath != c.RootDir && !strings.HasPrefix(lpath, c.RootDir+string(filepath.Separator)) {
			return "", fmt.Errorf("invalid content symlink: %s", path)
		}
		_, err = c.RealPath("/"+lrel, what)
		if err != nil {
			return "", err
		}
	}
	return rpath, nil
}

func (c *ContentValue) polishError(path starlark.String, err error) error {
	if e, ok := err.(*os.PathError); ok {
		e.Path = path.GoString()
	}
	return err
}

func contentValueRead(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (Value, error) {
	var path starlark.String
	err := starlark.UnpackArgs("Content.read", args, kwargs, "path", &path)
	if err != nil {
		return nil, err
	}
	recv := fn.Receiver().(*ContentValue)

	fpath, err := recv.RealPath(path.GoString(), CheckRead)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, recv.polishError(path, err)
	}
	return starlark.String(data), nil
}

func contentValueWrite(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (Value, error) {
	var path starlark.String
	var data starlark.String
	err := starlark.UnpackArgs("Content.write", args, kwargs, "path", &path, "data", &data)
	if err != nil {
		return nil, err
	}
	recv := fn.Receiver().(*ContentValue)

	fpath, err := recv.RealPath(path.GoString(), CheckWrite)
	if err != nil {
		return nil, err
	}
	fdata := []byte(data.GoString())

	// No mode parameter for now as slices are supposed to list files
	// explicitly instead.
	err = os.WriteFile(fpath, fdata, 0644)
	if err != nil {
		return nil, recv.polishError(path, err)
	}
	return starlark.None, nil
}

func contentValueList(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (Value, error) {
	var path starlark.String
	err := starlark.UnpackArgs("Content.list", args, kwargs, "path", &path)
	if err != nil {
		return nil, err
	}
	recv := fn.Receiver().(*ContentValue)

	dpath := path.GoString()
	if !strings.HasSuffix(dpath, "/") {
		dpath += "/"
	}
	fpath, err := recv.RealPath(dpath, CheckRead)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(fpath)
	if err != nil {
		return nil, recv.polishError(path, err)
	}
	values := make([]Value, len(entries))
	for i, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		values[i] = starlark.String(name)
	}
	return starlark.NewList(values), nil
}
