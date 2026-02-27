// Package typst provides Go bindings for the Typst markup language compiler.
// It compiles Typst source into PDF using the Typst Rust crate via CGO.
//
// Create a [Compiler] with [New], then call [Compiler.Compile] or
// [Compiler.CompileBytes]. Each Compiler is an independent instance
// with its own fonts and caches — safe for concurrent use and free
// of cross-instance contention.
package typst

/*
#cgo CFLAGS: -I${SRCDIR}/typst-ffi
#cgo LDFLAGS: -L${SRCDIR}/typst-ffi/target/release -ltypst_ffi -lm
#cgo darwin LDFLAGS: -liconv -framework CoreFoundation -framework Security
#cgo linux LDFLAGS: -lpthread -ldl

#include <stdlib.h>
#include "typst_ffi.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"
)

// CompileOption configures a single compilation call.
type CompileOption func(*compileConfig)

type compileConfig struct {
	root       string // directory for resolving #import and #image paths
	packageDir string // directory for resolving @preview/... package imports
}

// WithRoot sets the root directory for resolving local file imports and images.
// Paths in #import and #image() are resolved relative to this directory.
// Path traversal outside the root is blocked.
func WithRoot(dir string) CompileOption {
	return func(cfg *compileConfig) {
		cfg.root = dir
	}
}

// WithPackageDir overrides the default package cache directory.
// Typst packages are resolved at {dir}/{namespace}/{name}/{version}/.
func WithPackageDir(dir string) CompileOption {
	return func(cfg *compileConfig) {
		cfg.packageDir = dir
	}
}

var defaultPkgDir struct {
	once sync.Once
	dir  string
}

// DefaultPackageDir returns the platform-specific default Typst package cache directory.
// On Linux: ~/.cache/typst/packages/
// On macOS: ~/Library/Caches/typst/packages/
// The result is computed once and cached for the lifetime of the process.
func DefaultPackageDir() string {
	defaultPkgDir.once.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		if runtime.GOOS == "darwin" {
			defaultPkgDir.dir = filepath.Join(home, "Library", "Caches", "typst", "packages")
			return
		}
		// Linux and others: respect XDG_CACHE_HOME if set.
		if cacheDir := os.Getenv("XDG_CACHE_HOME"); cacheDir != "" {
			defaultPkgDir.dir = filepath.Join(cacheDir, "typst", "packages")
			return
		}
		defaultPkgDir.dir = filepath.Join(home, ".cache", "typst", "packages")
	})
	return defaultPkgDir.dir
}

// CompileError represents a Typst compilation error.
type CompileError struct {
	Message string
}

func (e *CompileError) Error() string {
	return e.Message
}

// Compiler is an independent Typst compiler instance with its own fonts
// and internal caches. It is safe for concurrent use from multiple goroutines.
//
// Create with [New] and free with [Compiler.Close].
type Compiler struct {
	world  *C.TypstWorld // pointer to Rust-allocated SharedResources (fonts + stdlib)
	once   sync.Once     // ensures free() runs at most once
	closed bool          // prevents compile after Close
}

// New creates a new Compiler. Bundled fonts (Libertinus Serif,
// New Computer Modern, DejaVu Sans Mono) are always loaded.
// Any additional font bytes (ttf/otf) are loaded on top.
//
// Multiple Compilers are fully independent — different fonts, no shared
// locks, no contention.
func New(fonts ...[]byte) (*Compiler, error) {
	var world *C.TypstWorld

	if len(fonts) == 0 {
		world = C.typst_world_new(nil, nil, 0)
	} else {
		n := len(fonts)
		// Allocate C arrays to pass font pointers and lengths across the FFI boundary.
		cPtrs := (*[1 << 30]*C.uint8_t)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof((*C.uint8_t)(nil)))))[:n:n]
		cLens := (*[1 << 30]C.size_t)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.size_t(0)))))[:n:n]
		defer C.free(unsafe.Pointer(&cPtrs[0]))
		defer C.free(unsafe.Pointer(&cLens[0]))

		for i, f := range fonts {
			if len(f) == 0 {
				cPtrs[i] = nil
				cLens[i] = 0
				continue
			}
			cPtrs[i] = (*C.uint8_t)(unsafe.Pointer(&f[0]))
			cLens[i] = C.size_t(len(f))
		}

		world = C.typst_world_new(
			(**C.uint8_t)(unsafe.Pointer(&cPtrs[0])),
			(*C.size_t)(unsafe.Pointer(&cLens[0])),
			C.size_t(n),
		)
	}

	if world == nil {
		return nil, errors.New("typst: failed to create compiler")
	}

	c := &Compiler{world: world}
	// Safety net: if caller forgets Close(), the GC will eventually free Rust memory.
	runtime.SetFinalizer(c, (*Compiler).free)
	return c, nil
}

// Compile reads Typst source from r and compiles it into a PDF.
// The returned [Document] directly references the compiled PDF in
// Rust-allocated memory with no copy. Call [Document.Close] when done.
func (c *Compiler) Compile(r io.Reader, opts ...CompileOption) (*Document, error) {
	source, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading typst source: %w", err)
	}
	return c.compile(source, opts)
}

// CompileBytes compiles Typst source bytes directly into a PDF.
// This is the fastest path — avoids the io.ReadAll allocation.
// The source slice is not retained after the call.
func (c *Compiler) CompileBytes(source []byte, opts ...CompileOption) (*Document, error) {
	return c.compile(source, opts)
}

// CompileFile reads and compiles a Typst file at the given path.
// The file's directory is automatically used as the root for resolving
// imports and images, unless overridden with [WithRoot].
func (c *Compiler) CompileFile(path string, opts ...CompileOption) (*Document, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	source, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading typst file: %w", err)
	}

	// Prepend WithRoot(dir) so user-supplied WithRoot can override it.
	dir := filepath.Dir(absPath)
	allOpts := make([]CompileOption, 0, len(opts)+1)
	allOpts = append(allOpts, WithRoot(dir))
	allOpts = append(allOpts, opts...)

	return c.compile(source, allOpts)
}

// compile is the shared implementation for Compile, CompileBytes, and CompileFile.
func (c *Compiler) compile(source []byte, opts []CompileOption) (*Document, error) {
	if c.closed {
		return nil, errors.New("typst: compiler is closed")
	}
	if len(source) == 0 {
		return nil, &CompileError{Message: "empty source"}
	}

	var cfg compileConfig
	for _, o := range opts {
		o(&cfg)
	}

	// Auto-detect default package dir if not explicitly set.
	if cfg.packageDir == "" {
		if dir := DefaultPackageDir(); dir != "" {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				cfg.packageDir = dir
			}
		}
	}

	// Prepare C pointers for root.
	// Use unsafe.StringData to avoid []byte(string) copy allocations —
	// the Rust side only reads these during the call.
	var rootPtr *C.uint8_t
	var rootLen C.size_t
	if cfg.root != "" {
		rootPtr = (*C.uint8_t)(unsafe.Pointer(unsafe.StringData(cfg.root)))
		rootLen = C.size_t(len(cfg.root))
	}

	// Prepare C pointers for package dir.
	var pkgPtr *C.uint8_t
	var pkgLen C.size_t
	if cfg.packageDir != "" {
		pkgPtr = (*C.uint8_t)(unsafe.Pointer(unsafe.StringData(cfg.packageDir)))
		pkgLen = C.size_t(len(cfg.packageDir))
	}

	result := C.typst_world_compile(
		c.world,
		(*C.uint8_t)(unsafe.Pointer(&source[0])),
		C.size_t(len(source)),
		rootPtr,
		rootLen,
		pkgPtr,
		pkgLen,
	)

	if result.error != 0 {
		// Copy error message to Go memory and free the Rust-allocated buffer.
		msg := C.GoBytes(unsafe.Pointer(result.data), C.int(result.len))
		C.typst_free_result(result.data, result.len)
		return nil, &CompileError{Message: string(msg)}
	}

	// Wrap the Rust-allocated PDF pointer in a Document; finalizer guards against leak.
	doc := &Document{
		data: result.data,
		len:  result.len,
	}
	runtime.SetFinalizer(doc, (*Document).free)
	return doc, nil
}

// Close frees the compiler and all its internal resources.
// After Close, Compile/CompileBytes return errors.
// Close is idempotent.
func (c *Compiler) Close() error {
	c.free()
	return nil
}

// free releases the Rust compiler. Idempotent via sync.Once.
func (c *Compiler) free() {
	c.once.Do(func() {
		if c.world != nil {
			C.typst_world_free(c.world)
		}
		c.world = nil
		c.closed = true
		runtime.SetFinalizer(c, nil)
	})
}

// Document holds the compiled PDF output backed by Rust-allocated memory.
// It provides zero-copy access to the PDF bytes.
//
// Close must be called when the document is no longer needed to free
// the underlying memory. After Close, all methods return errors and
// any byte slices previously returned by [Document.Bytes] are invalid.
type Document struct {
	data   *C.uint8_t // pointer to Rust-allocated PDF bytes
	len    C.size_t   // size of the PDF in bytes
	offset int        // current read position for io.Reader
	once   sync.Once  // ensures free() runs at most once
	closed bool       // prevents read/write after Close
}

// Len returns the size of the PDF in bytes.
func (d *Document) Len() int {
	if d.closed {
		return 0
	}
	return int(d.len)
}

// Bytes returns the raw PDF bytes backed directly by Rust-allocated memory.
// Zero-copy — no allocation or copying occurs.
//
// The returned slice is valid only until Close is called.
// Do not retain the slice beyond the lifetime of the Document.
func (d *Document) Bytes() []byte {
	if d.closed || d.len == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(d.data)), d.len)
}

// Read implements [io.Reader], reading from the PDF bytes.
func (d *Document) Read(p []byte) (int, error) {
	if d.closed {
		return 0, errors.New("typst: read on closed document")
	}
	buf := d.Bytes()
	if d.offset >= len(buf) {
		return 0, io.EOF
	}
	n := copy(p, buf[d.offset:])
	d.offset += n
	return n, nil
}

// WriteTo implements [io.WriterTo], writing the entire PDF to w.
// This writes directly from Rust-allocated memory with no intermediate copy.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	if d.closed {
		return 0, errors.New("typst: write on closed document")
	}
	buf := d.Bytes()
	n, err := w.Write(buf)
	return int64(n), err
}

// Close frees the underlying Rust-allocated memory.
// After Close, Bytes returns nil and Read/WriteTo return errors.
// Close is idempotent.
func (d *Document) Close() error {
	d.free()
	return nil
}

// free releases the Rust-allocated PDF memory. Idempotent via sync.Once.
func (d *Document) free() {
	d.once.Do(func() {
		if d.data != nil {
			C.typst_free_result(d.data, d.len)
		}
		d.data = nil
		d.len = 0
		d.closed = true
		runtime.SetFinalizer(d, nil)
	})
}
