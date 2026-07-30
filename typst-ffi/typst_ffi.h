#ifndef TYPST_FFI_H
#define TYPST_FFI_H

#include <stdint.h>
#include <stddef.h>

// Opaque handle to a compiler instance.
typedef struct TypstWorld TypstWorld;

typedef struct {
    uint8_t *data;
    size_t len;
    int32_t error;  // 0 = success, 1 = error
} TypstResult;

// Create a new compiler instance with optional custom fonts.
// Bundled fonts are always included. Custom fonts are added on top.
// Pass NULL/0 for no custom fonts.
// Returns a heap-allocated handle. Free with typst_world_free.
TypstWorld *typst_world_new(const uint8_t **font_ptrs, const size_t *font_lens, size_t font_count);

// Compile flags (bitmask). Keep in sync with the constants in src/lib.rs.
//
// TYPST_FLAG_TAGGED   : write a PDF structure tree (Tagged PDF) for a baseline
//                       of accessibility.
// TYPST_FLAG_PDF_UA_1 : enforce conformance with PDF/UA-1. Implies
//                       TYPST_FLAG_TAGGED, since PDF/UA requires tagging.
#define TYPST_FLAG_TAGGED   (1u << 0)
#define TYPST_FLAG_PDF_UA_1 (1u << 1)

// Compile a Typst source string to PDF.
// root_ptr/root_len: optional root directory for local file resolution (NULL/0 = disabled).
// pkg_ptr/pkg_len: optional package cache directory (NULL/0 = disabled).
// flags: bitwise OR of TYPST_FLAG_* (0 = untagged PDF 1.7, no standard enforced).
TypstResult typst_world_compile(const TypstWorld *world,
    const uint8_t *source_ptr, size_t source_len,
    const uint8_t *root_ptr, size_t root_len,
    const uint8_t *pkg_ptr, size_t pkg_len,
    uint32_t flags);

// Free a compiler instance.
void typst_world_free(TypstWorld *world);

// Free memory returned by typst_world_compile.
void typst_free_result(uint8_t *data, size_t len);

#endif // TYPST_FFI_H
