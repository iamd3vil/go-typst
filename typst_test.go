package typst

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func newTestCompiler(t *testing.T) *Compiler {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func testdataDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("failed to get testdata abs path: %v", err)
	}
	return abs
}

func TestCompile_simple(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.Compile(strings.NewReader(`= Hello, Typst!

This is a simple document.
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	if doc.Len() == 0 {
		t.Fatal("expected non-empty PDF output")
	}
	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatalf("output does not look like a PDF, starts with: %q", doc.Bytes()[:min(20, doc.Len())])
	}
}

func TestCompile_emptySource(t *testing.T) {
	c := newTestCompiler(t)
	_, err := c.Compile(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty source")
	}
	var ce *CompileError
	if !asCompileError(err, &ce) {
		t.Fatalf("expected CompileError, got %T: %v", err, err)
	}
}

func TestCompile_invalidTypst(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.Compile(strings.NewReader(`#let x = `))
	if err == nil {
		doc.Close()
	}
}

func TestCompileBytes(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte("Hello from bytes!"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestDocument_Read(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte("Hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	got, err := io.ReadAll(doc)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
	if len(got) != doc.Len() {
		t.Fatalf("ReadAll returned %d bytes, expected %d", len(got), doc.Len())
	}
}

func TestDocument_WriteTo(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte("Hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	var buf bytes.Buffer
	n, err := doc.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if int(n) != doc.Len() {
		t.Fatalf("WriteTo wrote %d bytes, expected %d", n, doc.Len())
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestDocument_CloseIdempotent(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte("Hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc.Close()
	doc.Close() // should not panic

	if doc.Len() != 0 {
		t.Fatal("expected 0 length after close")
	}
	if doc.Bytes() != nil {
		t.Fatal("expected nil bytes after close")
	}
}

func TestDocument_ReadAfterClose(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte("Hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	doc.Close()

	_, err = doc.Read(make([]byte, 10))
	if err == nil {
		t.Fatal("expected error reading closed document")
	}
}

func TestDocument_WriteTo_ioCopy(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.Compile(strings.NewReader(`
= Report

#lorem(200)
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	var buf bytes.Buffer
	n, err := io.Copy(&buf, doc)
	if err != nil {
		t.Fatalf("io.Copy error: %v", err)
	}
	if int(n) != doc.Len() {
		t.Fatalf("io.Copy wrote %d bytes, expected %d", n, doc.Len())
	}
}

func TestCompiler_CloseIdempotent(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close() // should not panic
}

func TestCompiler_CompileAfterClose(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	_, err = c.CompileBytes([]byte("Hello"))
	if err == nil {
		t.Fatal("expected error compiling with closed compiler")
	}
}

func TestMultipleCompilers(t *testing.T) {
	c1, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()

	c2, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	doc1, err := c1.CompileBytes([]byte("From compiler 1"))
	if err != nil {
		t.Fatal(err)
	}
	defer doc1.Close()

	doc2, err := c2.CompileBytes([]byte("From compiler 2"))
	if err != nil {
		t.Fatal(err)
	}
	defer doc2.Close()

	if !bytes.HasPrefix(doc1.Bytes(), []byte("%PDF-")) || !bytes.HasPrefix(doc2.Bytes(), []byte("%PDF-")) {
		t.Fatal("one or both outputs are not PDFs")
	}
}

func TestCompileFile(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileFile("testdata/sample.typ")
	if err != nil {
		t.Fatalf("CompileFile failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestImage(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileFile("testdata/with_image.typ")
	if err != nil {
		t.Fatalf("CompileFile with image failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestImport(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileFile("testdata/with_import.typ")
	if err != nil {
		t.Fatalf("CompileFile with import failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestImport_WithRoot(t *testing.T) {
	c := newTestCompiler(t)
	root := testdataDir(t)
	source := []byte(`#import "helper.typ": greet
#greet("WithRoot")
`)
	doc, err := c.CompileBytes(source, WithRoot(root))
	if err != nil {
		t.Fatalf("CompileBytes with WithRoot failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestImage_WithRoot(t *testing.T) {
	c := newTestCompiler(t)
	root := testdataDir(t)
	source := []byte(`#image("logo.png")`)
	doc, err := c.CompileBytes(source, WithRoot(root))
	if err != nil {
		t.Fatalf("CompileBytes with image via WithRoot failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestPathTraversal(t *testing.T) {
	c := newTestCompiler(t)
	root := testdataDir(t)
	source := []byte(`#image("../../etc/passwd")`)
	_, err := c.CompileBytes(source, WithRoot(root))
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestPackage(t *testing.T) {
	c := newTestCompiler(t)
	root := testdataDir(t)
	pkgDir := filepath.Join(root, "packages")
	source := []byte(`#import "@preview/example:0.1.0": example-func
#example-func()
`)
	doc, err := c.CompileBytes(source, WithRoot(root), WithPackageDir(pkgDir))
	if err != nil {
		t.Fatalf("CompileBytes with package import failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

func TestPackage_CompileFile(t *testing.T) {
	c := newTestCompiler(t)
	root := testdataDir(t)
	pkgDir := filepath.Join(root, "packages")
	doc, err := c.CompileFile("testdata/with_package.typ", WithPackageDir(pkgDir))
	if err != nil {
		t.Fatalf("CompileFile with package import failed: %v", err)
	}
	defer doc.Close()

	if !bytes.HasPrefix(doc.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
}

// accessibleSource is a document that satisfies the PDF/UA-1 requirements:
// it has a title, and its only figure carries alt text.
const accessibleSource = `#set document(title: [Accessible Report])

= Overview

Body text with a #link("https://typst.app")[link].

#image("logo.png", alt: "The project logo")
`

func TestCompile_defaultIsUntagged(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte(accessibleSource), WithRoot(testdataDir(t)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	if bytes.Contains(doc.Bytes(), []byte("StructTreeRoot")) {
		t.Error("default output should not contain a structure tree")
	}
}

func TestCompile_WithTaggedPDF(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte(accessibleSource), WithRoot(testdataDir(t)), WithTaggedPDF())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	b := doc.Bytes()
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
	for _, marker := range []string{"StructTreeRoot", "MarkInfo", "/Marked"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Errorf("tagged output is missing %q", marker)
		}
	}
	if bytes.Contains(b, []byte("pdfuaid")) {
		t.Error("tagged output should not claim PDF/UA conformance")
	}
}

func TestCompile_WithPDFUA1(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileBytes([]byte(accessibleSource), WithRoot(testdataDir(t)), WithPDFUA1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer doc.Close()

	b := doc.Bytes()
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatal("output does not look like a PDF")
	}
	// The XMP metadata must declare PDF/UA-1 conformance, and PDF/UA implies
	// tagging even though WithTaggedPDF was not passed.
	for _, marker := range []string{"pdfuaid", "StructTreeRoot"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Errorf("PDF/UA-1 output is missing %q", marker)
		}
	}
}

func TestCompile_WithPDFUA1_missingTitle(t *testing.T) {
	c := newTestCompiler(t)
	_, err := c.CompileBytes([]byte("= Heading\n\nNo document title set.\n"), WithPDFUA1())
	if err == nil {
		t.Fatal("expected error for PDF/UA-1 export without a document title")
	}
	var ce *CompileError
	if !asCompileError(err, &ce) {
		t.Fatalf("expected CompileError, got %T: %v", err, err)
	}
	if !strings.Contains(ce.Message, "missing document title") {
		t.Errorf("error should name the missing requirement, got: %q", ce.Message)
	}
	if !strings.Contains(ce.Message, "hint:") {
		t.Errorf("error should carry the actionable hint, got: %q", ce.Message)
	}
}

func TestCompile_WithPDFUA1_missingAltText(t *testing.T) {
	c := newTestCompiler(t)
	source := []byte("#set document(title: [T])\n#image(\"logo.png\")\n")
	_, err := c.CompileBytes(source, WithRoot(testdataDir(t)), WithPDFUA1())
	if err == nil {
		t.Fatal("expected error for PDF/UA-1 export with an image lacking alt text")
	}
	if !strings.Contains(err.Error(), "alt text") {
		t.Errorf("error should mention alt text, got: %q", err.Error())
	}
}

func TestCompileFile_WithPDFUA1(t *testing.T) {
	c := newTestCompiler(t)
	doc, err := c.CompileFile("testdata/accessible.typ", WithPDFUA1())
	if err != nil {
		t.Fatalf("CompileFile with WithPDFUA1 failed: %v", err)
	}
	defer doc.Close()

	if !bytes.Contains(doc.Bytes(), []byte("pdfuaid")) {
		t.Error("PDF/UA-1 output is missing the pdfuaid XMP marker")
	}
}

func asCompileError(err error, target **CompileError) bool {
	if ce, ok := err.(*CompileError); ok {
		*target = ce
		return true
	}
	return false
}
