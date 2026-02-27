package customfont_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	typst "github.com/iamd3vil/go-typst"
)

var (
	sampleSource []byte
	customSource []byte
	hugeSource   []byte
	fontsDir     string
	regularFont  []byte
	italicFont   []byte
)

func init() {
	var err error
	sampleSource, err = os.ReadFile("../../testdata/sample.typ")
	if err != nil {
		panic("reading sample source: " + err.Error())
	}
	customSource, err = os.ReadFile("../../testdata/custom_font.typ")
	if err != nil {
		panic("reading custom font source: " + err.Error())
	}
	hugeSource, err = os.ReadFile("../../testdata/huge_table.typ")
	if err != nil {
		panic("reading huge table source: " + err.Error())
	}
	regularFont, err = os.ReadFile("../../testdata/fonts/InterVariable.ttf")
	if err != nil {
		panic("reading font: " + err.Error())
	}
	italicFont, err = os.ReadFile("../../testdata/fonts/InterVariable-Italic.ttf")
	if err != nil {
		panic("reading font: " + err.Error())
	}
	fontsDir, _ = filepath.Abs("../../testdata/fonts")
}

func newCompiler(tb testing.TB) *typst.Compiler {
	tb.Helper()
	c, err := typst.New(regularFont, italicFont)
	if err != nil {
		tb.Fatalf("New() failed: %v", err)
	}
	tb.Cleanup(func() { c.Close() })
	return c
}

// --- Unit test ---

func TestInit_CustomFonts(t *testing.T) {
	c := newCompiler(t)

	doc, err := c.CompileBytes(customSource)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	defer doc.Close()

	pdf := doc.Bytes()
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}

	os.WriteFile("../../testdata/custom_font.pdf", pdf, 0644)
	t.Logf("PDF size: %d bytes (written to testdata/custom_font.pdf)", doc.Len())
}

// --- Serial benchmarks ---

func BenchmarkBundledFont_Library(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(sampleSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	b.Logf("PDF size: %d bytes", doc.Len())
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		doc, err := c.CompileBytes(sampleSource)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}
		doc.Close()
	}
}

func BenchmarkCustomFont_Library(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(customSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	b.Logf("PDF size: %d bytes", doc.Len())
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		doc, err := c.CompileBytes(customSource)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}
		doc.Close()
	}
}

func BenchmarkHugeTable_Library(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(hugeSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	b.Logf("PDF size: %d bytes (%d MB)", doc.Len(), doc.Len()/1024/1024)
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		doc, err := c.CompileBytes(hugeSource)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}
		doc.Close()
	}
}

// --- CLI benchmarks ---

func BenchmarkBundledFont_CLI(b *testing.B) {
	typstBin, err := exec.LookPath("typst")
	if err != nil {
		b.Skip("typst CLI not found")
	}

	srcPath, _ := filepath.Abs("../../testdata/sample.typ")
	outPath := filepath.Join(b.TempDir(), "output.pdf")

	cmd := exec.Command(typstBin, "compile", srcPath, outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("warmup failed: %v\n%s", err, out)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cmd := exec.Command(typstBin, "compile", srcPath, outPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("compile failed: %v\n%s", err, out)
		}
	}
}

func BenchmarkCustomFont_CLI(b *testing.B) {
	typstBin, err := exec.LookPath("typst")
	if err != nil {
		b.Skip("typst CLI not found")
	}

	srcPath, _ := filepath.Abs("../../testdata/custom_font.typ")
	outPath := filepath.Join(b.TempDir(), "output.pdf")

	cmd := exec.Command(typstBin, "compile", "--font-path", fontsDir, srcPath, outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("warmup failed: %v\n%s", err, out)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cmd := exec.Command(typstBin, "compile", "--font-path", fontsDir, srcPath, outPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("compile failed: %v\n%s", err, out)
		}
	}
}

func BenchmarkHugeTable_CLI(b *testing.B) {
	typstBin, err := exec.LookPath("typst")
	if err != nil {
		b.Skip("typst CLI not found")
	}

	srcPath, _ := filepath.Abs("../../testdata/huge_table.typ")
	outPath := filepath.Join(b.TempDir(), "output.pdf")

	cmd := exec.Command(typstBin, "compile", srcPath, outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("warmup failed: %v\n%s", err, out)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cmd := exec.Command(typstBin, "compile", srcPath, outPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("compile failed: %v\n%s", err, out)
		}
	}
}

// --- Parallel benchmarks: single shared compiler ---

func BenchmarkBundledFont_Parallel_SharedCompiler(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(sampleSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			doc, err := c.CompileBytes(sampleSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}

func BenchmarkCustomFont_Parallel_SharedCompiler(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(customSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			doc, err := c.CompileBytes(customSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}

func BenchmarkHugeTable_Parallel_SharedCompiler(b *testing.B) {
	c := newCompiler(b)

	doc, err := c.CompileBytes(hugeSource)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	doc.Close()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			doc, err := c.CompileBytes(hugeSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}

// --- Parallel benchmarks: one compiler per CPU ---

func BenchmarkBundledFont_Parallel_CompilerPerCPU(b *testing.B) {
	nCPU := runtime.GOMAXPROCS(0)
	compilers := make([]*typst.Compiler, nCPU)
	for i := range compilers {
		c, err := typst.New(regularFont, italicFont)
		if err != nil {
			b.Fatal(err)
		}
		// Warmup each compiler.
		doc, err := c.CompileBytes(sampleSource)
		if err != nil {
			b.Fatal(err)
		}
		doc.Close()
		compilers[i] = c
	}
	b.Cleanup(func() {
		for _, c := range compilers {
			c.Close()
		}
	})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c := compilers[i%nCPU]
			i++
			doc, err := c.CompileBytes(sampleSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}

func BenchmarkCustomFont_Parallel_CompilerPerCPU(b *testing.B) {
	nCPU := runtime.GOMAXPROCS(0)
	compilers := make([]*typst.Compiler, nCPU)
	for i := range compilers {
		c, err := typst.New(regularFont, italicFont)
		if err != nil {
			b.Fatal(err)
		}
		doc, err := c.CompileBytes(customSource)
		if err != nil {
			b.Fatal(err)
		}
		doc.Close()
		compilers[i] = c
	}
	b.Cleanup(func() {
		for _, c := range compilers {
			c.Close()
		}
	})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c := compilers[i%nCPU]
			i++
			doc, err := c.CompileBytes(customSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}

func BenchmarkHugeTable_Parallel_CompilerPerCPU(b *testing.B) {
	nCPU := runtime.GOMAXPROCS(0)
	compilers := make([]*typst.Compiler, nCPU)
	for i := range compilers {
		c, err := typst.New(regularFont, italicFont)
		if err != nil {
			b.Fatal(err)
		}
		doc, err := c.CompileBytes(hugeSource)
		if err != nil {
			b.Fatal(err)
		}
		doc.Close()
		compilers[i] = c
	}
	b.Cleanup(func() {
		for _, c := range compilers {
			c.Close()
		}
	})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c := compilers[i%nCPU]
			i++
			doc, err := c.CompileBytes(hugeSource)
			if err != nil {
				b.Fatalf("compile failed: %v", err)
			}
			doc.Close()
		}
	})
}
