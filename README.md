# go-typst

A Go library for compiling [Typst](https://typst.app) markup into PDF, powered by the Typst Rust crate (v0.15.1) via CGO.

- **Zero-copy output** — PDF bytes stay in Rust memory, never copied to the Go heap.
- **Instance-based** — each `Compiler` has its own fonts and caches, safe for concurrent use.
- **Custom fonts** — load any TTF/OTF when creating a `Compiler`, alongside the bundled defaults.
- **File & import support** — `#import`, `#image()`, and 3rd-party packages work via `WithRoot` and `WithPackageDir` options.
- **Accessibility** — opt into Tagged PDF or full PDF/UA-1 conformance with `WithTaggedPDF` / `WithPDFUA1`.

## Prerequisites

- **Rust** toolchain (`rustc`, `cargo`) — [install via rustup](https://rustup.rs)
- **Go** 1.21+
- **C compiler** (for CGO linking)

## Build

Build the Rust static library first:

```bash
make rust
```

## Usage

### Basic

```go
package main

import (
	"fmt"
	"os"
	"strings"

	typst "github.com/iamd3vil/go-typst"
)

func main() {
	// Create a compiler (bundled fonts only).
	c, err := typst.New()
	if err != nil {
		panic(err)
	}
	defer c.Close()

	// Compile from an io.Reader.
	doc, err := c.Compile(strings.NewReader(`
= My Document

Hello from *Typst* compiled via Go!

$ sum_(k=1)^n k = (n(n+1))/2 $
`))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer doc.Close()

	// Zero-copy write directly from Rust memory to file.
	f, _ := os.Create("output.pdf")
	defer f.Close()
	doc.WriteTo(f)

	fmt.Printf("Written %d bytes to output.pdf\n", doc.Len())
}
```

### Custom Fonts

```go
regular, _ := os.ReadFile("fonts/Inter.ttf")
italic, _ := os.ReadFile("fonts/Inter-Italic.ttf")

// Bundled fonts are always included; custom fonts are added on top.
c, err := typst.New(regular, italic)
if err != nil {
    panic(err)
}
defer c.Close()

doc, _ := c.CompileBytes([]byte(`#set text(font: "Inter"); Hello!`))
defer doc.Close()
```

### Compiling Files with Imports & Images

```go
c, _ := typst.New()
defer c.Close()

// CompileFile auto-derives root from the file's directory.
doc, err := c.CompileFile("docs/report.typ")
if err != nil {
    panic(err)
}
defer doc.Close()
```

Any `#import "helper.typ"` or `#image("logo.png")` in `report.typ` will resolve relative to `docs/`.

### Using Typst Packages

```go
// Packages from the system cache (~/.cache/typst/packages/ on Linux,
// ~/Library/Caches/typst/packages/ on macOS) are auto-detected.
doc, _ := c.CompileFile("docs/report.typ")

// Or point to a custom package directory:
doc, _ = c.CompileBytes(source, typst.WithPackageDir("/path/to/packages"))
```

### WithRoot for Inline Source

```go
// When compiling source bytes (not a file), set root explicitly:
source := []byte(`#image("logo.png")`)
doc, _ := c.CompileBytes(source, typst.WithRoot("/path/to/assets"))
```

### Accessibility: Tagged PDF & PDF/UA-1

By default the PDF is untagged — the smallest, fastest output. Accessibility is
opt-in per compilation:

```go
// Tagged PDF: writes a structure tree (headings, lists, tables, links,
// figures) so screen readers can navigate the document semantically.
doc, err := c.CompileBytes(source, typst.WithTaggedPDF())

// PDF/UA-1 (ISO 14289-1): tagging plus conformance validation and the
// pdfuaid XMP marker. Implies WithTaggedPDF.
doc, err = c.CompileBytes(source, typst.WithPDFUA1())
```

`WithPDFUA1` makes Typst validate the document during export, so a document
that cannot conform fails to compile:

```go
_, err := c.CompileBytes([]byte("= Heading\n\nNo title."), typst.WithPDFUA1())
// err: pdf export error: PDF/UA-1 error: missing document title
//        hint: set the title with `set document(title: [...])`
```

To satisfy PDF/UA-1, the source must set a document title and give every image
and equation alt text:

```typst
#set document(title: [Accessible Report])
#set text(lang: "en")

= Introduction

#image("logo.png", alt: "The project logo")
```

### Multiple Independent Compilers

```go
// Each compiler has its own fonts and caches — no shared state.
invoiceCompiler, _ := typst.New(invoiceFonts...)
reportCompiler, _ := typst.New(reportFonts...)

// Safe to use concurrently from different goroutines.
go func() { invoiceCompiler.CompileBytes(invoiceSource) }()
go func() { reportCompiler.CompileBytes(reportSource) }()
```

## API

### `func New(fonts ...[]byte) (*Compiler, error)`

Creates a new independent compiler instance. Bundled fonts (Libertinus Serif, New Computer Modern, DejaVu Sans Mono) are always loaded. Any additional font bytes passed here (TTF/OTF) are loaded on top.

### `type Compiler`

```go
func (c *Compiler) Compile(r io.Reader, opts ...CompileOption) (*Document, error)
func (c *Compiler) CompileBytes(source []byte, opts ...CompileOption) (*Document, error)
func (c *Compiler) CompileFile(path string, opts ...CompileOption) (*Document, error)
func (c *Compiler) Close() error
```

- **`Compile(r, opts...)`** — reads all bytes from `r`, compiles to PDF.
- **`CompileBytes(b, opts...)`** — compiles directly from a byte slice. Fastest path — avoids `io.ReadAll`.
- **`CompileFile(path, opts...)`** — reads and compiles a `.typ` file. The file's directory is automatically used as root for resolving imports and images, unless overridden with `WithRoot`.
- **`Close()`** — frees the compiler and all its internal resources. Idempotent. A runtime finalizer acts as safety net.

A `Compiler` is safe for concurrent use from multiple goroutines.

### `type CompileOption`

```go
func WithRoot(dir string) CompileOption
func WithPackageDir(dir string) CompileOption
func WithTaggedPDF() CompileOption
func WithPDFUA1() CompileOption
```

- **`WithRoot(dir)`** — sets the root directory for resolving `#import` and `#image()` paths. Path traversal outside the root is blocked.
- **`WithPackageDir(dir)`** — overrides the default package cache directory. Packages are resolved at `{dir}/{namespace}/{name}/{version}/`.
- **`WithTaggedPDF()`** — writes a PDF structure tree (Tagged PDF) for a baseline of accessibility. Off by default: it makes the output larger and slower to produce.
- **`WithPDFUA1()`** — enforces PDF/UA-1 (ISO 14289-1) conformance; implies `WithTaggedPDF()`. Export fails with a `*CompileError` (message plus hints) if the document cannot conform — e.g. a missing document title or missing image alt text.

### `func DefaultPackageDir() string`

Returns the platform-specific default Typst package cache directory (`~/.cache/typst/packages/` on Linux, `~/Library/Caches/typst/packages/` on macOS). Respects `XDG_CACHE_HOME`.

### `type Document`

```go
func (d *Document) Bytes() []byte                       // zero-copy view into Rust memory
func (d *Document) Len() int                            // PDF size in bytes
func (d *Document) Read(p []byte) (int, error)          // io.Reader
func (d *Document) WriteTo(w io.Writer) (int64, error)  // io.WriterTo (zero-copy)
func (d *Document) Close() error                        // frees Rust memory
```

- **`Bytes()`** — returns a slice backed directly by Rust-allocated memory. No allocation, no copy. Valid until `Close()`.
- **`WriteTo(w)`** — writes the PDF directly from Rust memory to `w`. Fastest path for writing to a file — single write, no Go heap allocation.
- **`Read(p)`** — standard `io.Reader`. Works with `io.Copy` etc.
- **`Close()`** — frees the underlying Rust memory. Idempotent.

### `type CompileError`

```go
type CompileError struct {
    Message string
}
```

Returned when Typst compilation or PDF export fails.

## Memory Model

```
New(fonts...)
  └─ Rust: parses fonts, builds library → heap-allocated Compiler instance

c.CompileBytes(source)
  ├─ Rust: copies source, compiles → Rust-allocated PDF bytes
  └─ Returns *Document pointing directly at Rust memory (zero-copy)

doc.WriteTo(file)
  └─ Writes from Rust memory → fd (single write syscall, no Go allocation)

doc.Close()
  └─ Frees Rust-allocated PDF memory

c.Close()
  └─ Frees compiler (fonts, library, caches)
```

The only copy is the source input. The PDF output is never copied into Go heap memory.

## Benchmarks

Measured on Apple M3 Pro (11 cores), Typst 0.14.2, `go test -bench`:

### Serial (single goroutine)

| Document | Library | CLI | Speedup | Docs/sec |
|---|---|---|---|---|
| 1-page (custom font, 19 KB) | **0.25 ms** | 57 ms | **228×** | 3,960 |
| 5-page report (109 KB) | **2.22 ms** | 90 ms | **41×** | 450 |
| 1000-page table (94 MB) | **0.97 s** | 9.3 s | **9.6×** | 1.03 |

### Parallel (GOMAXPROCS=11)

| Document | Shared Compiler | Compiler-per-CPU | Docs/sec |
|---|---|---|---|
| 1-page (custom font) | 0.246 ms/op | 0.245 ms/op | **~44,900** |
| 5-page report | 8.50 ms/op | 8.50 ms/op | **~1,295** |
| 1000-page table | 0.64 s/op | 0.63 s/op | **~5.3** |

Go-side allocations: **1 alloc, 48 bytes** per compile (just the `Document` struct).

## Building & Distribution

Rust is only needed **once** to build the static library. The final Go binary is fully self-contained — no Rust runtime, no shared libraries, no external dependencies.

```bash
# 1. Build the Rust static library (one time, or in CI)
make rust

# 2. Build your Go binary as usual — the Typst compiler is statically linked in
go build -o myapp ./cmd/myapp

# The resulting binary is standalone. Ship it anywhere.
./myapp
```

To distribute to teammates or CI environments that don't have Rust:

1. Build `libtypst_ffi.a` once on a machine with Rust (or in CI).
2. Commit it to your project or store it as a build artifact.
3. Point the CGO linker at it — consumers only need Go and a C compiler.

```bash
# Example: copy the prebuilt library into your project
cp path/to/go-typst/typst-ffi/target/release/libtypst_ffi.a ./vendor-lib/

# Build with custom library path
CGO_LDFLAGS="-L./vendor-lib -ltypst_ffi -lm -liconv -framework CoreFoundation -framework Security" \
    go build ./...
```

For cross-compilation, build the Rust library for each target:

```bash
# Example: build for linux/amd64 from macOS
rustup target add x86_64-unknown-linux-gnu
cd typst-ffi && cargo build --release --target x86_64-unknown-linux-gnu
# Output: typst-ffi/target/x86_64-unknown-linux-gnu/release/libtypst_ffi.a
```

## Testing

```bash
make test
```

## Limitations

- **No automatic package download**: packages must already be in the cache (e.g. installed via the `typst` CLI). This library resolves packages from the local filesystem only.
- **Fonts fixed at creation**: fonts are loaded once when the `Compiler` is created via `New()` and cannot be changed afterwards.
- **One PDF standard exposed**: of the standards Typst can enforce, only PDF/UA-1 is wired through (see `WithPDFUA1`). The PDF/A family is not exposed.
- **macOS/Linux**: tested on macOS (arm64) and Linux (amd64).
