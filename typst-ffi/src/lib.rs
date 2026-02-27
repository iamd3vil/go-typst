#![allow(private_interfaces)]

use std::fmt::Write;
use std::path::PathBuf;
use std::slice;

use chrono::{Datelike, Local};
use typst::diag::{FileError, FileResult};
use typst::foundations::{Bytes, Datetime};
use typst::layout::PagedDocument;
use typst::syntax::{FileId, Source, VirtualPath};
use typst::text::{Font, FontBook};
use typst::utils::LazyHash;
use typst::{Library, LibraryExt, World};

/// Shared, immutable resources owned by a compiler instance.
pub struct SharedResources {
    library: LazyHash<Library>,
    book: LazyHash<FontBook>,
    fonts: Vec<Font>,
    main_id: FileId,
}

impl SharedResources {
    fn new(custom_font_data: &[&[u8]]) -> Self {
        // Pre-allocate: bundled fonts typically yield ~20 faces,
        // each custom file usually contains 1-4 faces.
        let mut fonts = Vec::with_capacity(20 + custom_font_data.len() * 4);

        // Load bundled fonts.
        for data in typst_assets::fonts() {
            let bytes = Bytes::new(data);
            for index in 0.. {
                match Font::new(bytes.clone(), index) {
                    Some(font) => fonts.push(font),
                    None => break,
                }
            }
        }

        // Load custom fonts.
        for data in custom_font_data {
            let bytes = Bytes::new(data.to_vec());
            for index in 0.. {
                match Font::new(bytes.clone(), index) {
                    Some(font) => fonts.push(font),
                    None => break,
                }
            }
        }

        let mut book = FontBook::new();
        for font in &fonts {
            book.push(font.info().clone());
        }

        SharedResources {
            library: LazyHash::new(Library::default()),
            book: LazyHash::new(book),
            fonts,
            main_id: FileId::new(None, VirtualPath::new("/main.typ")),
        }
    }
}

/// A minimal World that borrows shared resources and owns a single source.
struct SingleSourceWorld<'a> {
    shared: &'a SharedResources,
    source: Source,
    root: Option<PathBuf>,
    canonical_root: Option<PathBuf>,
    package_cache: Option<PathBuf>,
}

impl<'a> SingleSourceWorld<'a> {
    fn new(
        shared: &'a SharedResources,
        source_text: String,
        root: Option<PathBuf>,
        package_cache: Option<PathBuf>,
    ) -> Self {
        // Pre-compute canonical root once to avoid repeated canonicalize() in resolve_path.
        let canonical_root = root.as_ref().and_then(|r| r.canonicalize().ok());
        SingleSourceWorld {
            shared,
            source: Source::new(shared.main_id, source_text),
            root,
            canonical_root,
            package_cache,
        }
    }

    /// Resolve a FileId to an absolute path on disk, with path traversal protection.
    fn resolve_path(&self, id: FileId) -> FileResult<PathBuf> {
        let vpath = id.vpath().as_rootless_path();

        let (base, canonical_base) = if let Some(pkg) = id.package() {
            // Package file: {cache}/{namespace}/{name}/{version}/
            let cache = self
                .package_cache
                .as_ref()
                .ok_or_else(|| FileError::NotFound(vpath.into()))?;
            let b = cache
                .join(pkg.namespace.as_str())
                .join(pkg.name.as_str())
                .join(pkg.version.to_string());
            let cb = b
                .canonicalize()
                .map_err(|_| FileError::NotFound(vpath.into()))?;
            (b, cb)
        } else {
            // Local file: resolve relative to root.
            let b = self
                .root
                .as_ref()
                .ok_or_else(|| FileError::NotFound(vpath.into()))?
                .clone();
            let cb = self
                .canonical_root
                .as_ref()
                .ok_or_else(|| FileError::NotFound(vpath.into()))?
                .clone();
            (b, cb)
        };

        let full = base.join(vpath);

        // Canonicalize to resolve symlinks and ../ components.
        let canonical = full
            .canonicalize()
            .map_err(|_| FileError::NotFound(vpath.into()))?;

        // Path traversal protection: ensure resolved path is within base.
        if !canonical.starts_with(&canonical_base) {
            return Err(FileError::AccessDenied);
        }

        Ok(canonical)
    }
}

impl World for SingleSourceWorld<'_> {
    fn library(&self) -> &LazyHash<Library> {
        &self.shared.library
    }

    fn book(&self) -> &LazyHash<FontBook> {
        &self.shared.book
    }

    fn main(&self) -> FileId {
        self.source.id()
    }

    fn source(&self, id: FileId) -> FileResult<Source> {
        if id == self.source.id() {
            return Ok(self.source.clone());
        }
        let path = self.resolve_path(id)?;
        let text = std::fs::read_to_string(&path)
            .map_err(|_| FileError::NotFound(id.vpath().as_rootless_path().into()))?;
        Ok(Source::new(id, text))
    }

    fn file(&self, id: FileId) -> FileResult<Bytes> {
        let path = self.resolve_path(id)?;
        let data = std::fs::read(&path)
            .map_err(|_| FileError::NotFound(id.vpath().as_rootless_path().into()))?;
        Ok(Bytes::new(data))
    }

    fn font(&self, index: usize) -> Option<Font> {
        self.shared.fonts.get(index).cloned()
    }

    fn today(&self, offset: Option<i64>) -> Option<Datetime> {
        let now = Local::now();
        let naive = match offset {
            None => now.naive_local(),
            Some(o) => {
                let utc = now.naive_utc();
                utc + chrono::Duration::hours(o)
            }
        };
        Datetime::from_ymd(
            naive.year(),
            naive.month().try_into().ok()?,
            naive.day().try_into().ok()?,
        )
    }
}

// ---------------------------------------------------------------------------
// FFI
// ---------------------------------------------------------------------------

/// Opaque handle to a compiler instance.
pub type TypstWorld = SharedResources;

/// Result from compilation.
#[repr(C)]
pub struct TypstResult {
    pub data: *mut u8,
    pub len: usize,
    /// 0 = success, 1 = error.
    pub error: i32,
}

/// Create a new compiler instance with optional custom fonts.
///
/// # Safety
/// Each `font_ptrs[i]` must point to `font_lens[i]` valid bytes.
/// Returns a heap-allocated handle. Free with `typst_world_free`.
#[no_mangle]
pub unsafe extern "C" fn typst_world_new(
    font_ptrs: *const *const u8,
    font_lens: *const usize,
    font_count: usize,
) -> *mut TypstWorld {
    let custom: Vec<&[u8]> = if font_count > 0 && !font_ptrs.is_null() && !font_lens.is_null() {
        let ptrs = unsafe { slice::from_raw_parts(font_ptrs, font_count) };
        let lens = unsafe { slice::from_raw_parts(font_lens, font_count) };
        ptrs.iter()
            .zip(lens.iter())
            .map(|(&ptr, &len)| unsafe { slice::from_raw_parts(ptr, len) })
            .collect()
    } else {
        Vec::new()
    };

    let resources = SharedResources::new(&custom);
    Box::into_raw(Box::new(resources))
}

/// Compile a Typst source string to PDF using the given compiler instance.
///
/// # Safety
/// - `world` must be a valid pointer from `typst_world_new`.
/// - `source_ptr` must point to `source_len` valid UTF-8 bytes.
/// - `root_ptr`/`root_len`: optional root directory for local file resolution (NULL/0 = disabled).
/// - `pkg_ptr`/`pkg_len`: optional package cache directory (NULL/0 = disabled).
/// - Free the result with `typst_free_result`.
#[no_mangle]
pub unsafe extern "C" fn typst_world_compile(
    world: *const TypstWorld,
    source_ptr: *const u8,
    source_len: usize,
    root_ptr: *const u8,
    root_len: usize,
    pkg_ptr: *const u8,
    pkg_len: usize,
) -> TypstResult {
    let shared = unsafe { &*world };

    let source_bytes = unsafe { slice::from_raw_parts(source_ptr, source_len) };
    let source_text = match std::str::from_utf8(source_bytes) {
        Ok(s) => s.to_string(),
        Err(e) => {
            return make_error(format!("invalid UTF-8 input: {}", e));
        }
    };

    let root = if !root_ptr.is_null() && root_len > 0 {
        let bytes = unsafe { slice::from_raw_parts(root_ptr, root_len) };
        match std::str::from_utf8(bytes) {
            Ok(s) => Some(PathBuf::from(s)),
            Err(_) => None,
        }
    } else {
        None
    };

    let package_cache = if !pkg_ptr.is_null() && pkg_len > 0 {
        let bytes = unsafe { slice::from_raw_parts(pkg_ptr, pkg_len) };
        match std::str::from_utf8(bytes) {
            Ok(s) => Some(PathBuf::from(s)),
            Err(_) => None,
        }
    } else {
        None
    };

    let world = SingleSourceWorld::new(shared, source_text, root, package_cache);
    let result = typst::compile::<PagedDocument>(&world);

    match result.output {
        Ok(document) => {
            let options = typst_pdf::PdfOptions {
                tagged: false,
                ..typst_pdf::PdfOptions::default()
            };
            match typst_pdf::pdf(&document, &options) {
                Ok(pdf_bytes) => {
                    // Leak the PDF bytes into C-owned memory; Go will free via typst_free_result.
                    let mut boxed = pdf_bytes.into_boxed_slice();
                    let ptr = boxed.as_mut_ptr();
                    let len = boxed.len();
                    std::mem::forget(boxed);
                    TypstResult {
                        data: ptr,
                        len,
                        error: 0,
                    }
                }
                Err(errors) => {
                    let mut msg =
                        String::with_capacity((result.warnings.len() + errors.len()) * 64);
                    for w in result.warnings.iter() {
                        let _ = write!(msg, "warning: {}\n", w.message);
                    }
                    for err in errors.iter() {
                        let _ = write!(msg, "pdf export error: {}\n", err.message);
                    }
                    make_error(msg)
                }
            }
        }
        Err(errors) => {
            let mut msg = String::with_capacity((result.warnings.len() + errors.len()) * 64);
            for w in result.warnings.iter() {
                let _ = write!(msg, "warning: {}\n", w.message);
            }
            for err in errors.iter() {
                let _ = write!(msg, "compile error: {}\n", err.message);
            }
            make_error(msg)
        }
    }
}

/// Free a compiler instance.
///
/// # Safety
/// `world` must be a valid pointer from `typst_world_new`, or null.
#[no_mangle]
pub unsafe extern "C" fn typst_world_free(world: *mut TypstWorld) {
    if !world.is_null() {
        let _ = unsafe { Box::from_raw(world) };
    }
}

/// Free memory allocated by `typst_world_compile`.
///
/// # Safety
/// `data` and `len` must come from a previous `TypstResult`.
#[no_mangle]
pub unsafe extern "C" fn typst_free_result(data: *mut u8, len: usize) {
    if !data.is_null() && len > 0 {
        // Reconstruct the Vec from the leaked pointer and drop it to free the memory.
        let _ = unsafe { Vec::from_raw_parts(data, len, len) };
    }
}

/// Convert an error message into a TypstResult with error flag set.
/// The message bytes are leaked into C-owned memory for Go to read and free.
fn make_error(msg: String) -> TypstResult {
    let mut bytes = msg.into_bytes().into_boxed_slice();
    let ptr = bytes.as_mut_ptr();
    let len = bytes.len();
    std::mem::forget(bytes);
    TypstResult {
        data: ptr,
        len,
        error: 1,
    }
}
