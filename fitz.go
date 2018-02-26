// Package fitz provides wrapper for the [MuPDF](http://mupdf.com/) that can extract images from PDF, EPUB and XPS documents.
package fitz

/*
#include <mupdf/fitz.h>
#include <stdlib.h>

#cgo CFLAGS: -Iinclude

#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/libs -lmupdf_linux_amd64 -lmupdfthird_linux_amd64 -lm
#cgo linux,!android,arm LDFLAGS: -L${SRCDIR}/libs -lmupdf_linux_arm -lmupdfthird_linux_arm -lm
#cgo linux,!android,arm64 LDFLAGS: -L${SRCDIR}/libs -lmupdf_linux_arm64 -lmupdfthird_linux_arm64 -lm
#cgo android,arm LDFLAGS: -L${SRCDIR}/libs -lmupdf_android_arm -lmupdfthird_android_arm -lm
#cgo android,arm64 LDFLAGS: -L${SRCDIR}/libs -lmupdf_android_arm64 -lmupdfthird_android_arm64 -lm
#cgo windows,386 LDFLAGS: -L${SRCDIR}/libs -lmupdf_windows_386 -lmupdfthird_windows_386 -lm
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/libs -lmupdf_windows_amd64 -lmupdfthird_windows_amd64 -lm
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/libs -lmupdf_darwin_amd64 -lmupdfthird_darwin_amd64 -lm

const char *fz_version = FZ_VERSION;
*/
import "C"

import (
	"errors"
	"image"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"unsafe"
)

// Errors
var (
	ErrNoSuchFile    = errors.New("fitz: no such file")
	ErrCreateContext = errors.New("fitz: cannot create context")
	ErrOpenDocument  = errors.New("fitz: cannot open document")
	ErrOpenMemory    = errors.New("fitz: cannot open memory")
	ErrPageMissing   = errors.New("fitz: page missing")
	ErrCreatePixmap  = errors.New("fitz: cannot create pixmap")
	ErrPixmapSamples = errors.New("fitz: cannot get pixmap samples")
	ErrNeedsPassword = errors.New("fitz: document needs password")
)

// Document represents fitz document
type Document struct {
	ctx *C.struct_fz_context_s
	doc *C.struct_fz_document_s
}

// New returns new fitz document.
func New(filename string) (f *Document, err error) {
	f = &Document{}

	filename, err = filepath.Abs(filename)
	if err != nil {
		return
	}

	if _, e := os.Stat(filename); e != nil {
		err = ErrNoSuchFile
		return
	}

	f.ctx = (*C.struct_fz_context_s)(unsafe.Pointer(C.fz_new_context_imp(nil, nil, C.FZ_STORE_UNLIMITED, C.fz_version)))
	if f.ctx == nil {
		err = ErrCreateContext
		return
	}

	C.fz_register_document_handlers(f.ctx)

	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))

	f.doc = C.fz_open_document(f.ctx, cfilename)
	if f.doc == nil {
		err = ErrOpenDocument
	}

	ret := C.fz_needs_password(f.ctx, f.doc)
	v := bool(int(ret) != 0)
	if v {
		err = ErrNeedsPassword
	}

	return
}

// NewFromMemory returns new fitz document from byte slice.
func NewFromMemory(b []byte) (f *Document, err error) {
	f = &Document{}

	f.ctx = (*C.struct_fz_context_s)(unsafe.Pointer(C.fz_new_context_imp(nil, nil, C.FZ_STORE_UNLIMITED, C.fz_version)))
	if f.ctx == nil {
		err = ErrCreateContext
		return
	}

	C.fz_register_document_handlers(f.ctx)

	data := (*C.uchar)(C.CBytes(b))

	stream := C.fz_open_memory(f.ctx, data, C.ulong(len(b)))
	if stream == nil {
		err = ErrOpenMemory
		return
	}

	cmagic := C.CString("application/pdf")
	defer C.free(unsafe.Pointer(cmagic))

	f.doc = C.fz_open_document_with_stream(f.ctx, cmagic, stream)
	if f.doc == nil {
		err = ErrOpenDocument
	}

	ret := C.fz_needs_password(f.ctx, f.doc)
	v := bool(int(ret) != 0)
	if v {
		err = ErrNeedsPassword
	}

	return
}

// NewFromReader returns new fitz document from io.Reader.
func NewFromReader(r io.Reader) (f *Document, err error) {
	b, e := ioutil.ReadAll(r)
	if e != nil {
		err = e
		return
	}

	f, err = NewFromMemory(b)

	return
}

// NumPage returns total number of pages in document
func (f *Document) NumPage() int {
	return int(C.fz_count_pages(f.ctx, f.doc))
}

// Image returns image for given page number.
func (f *Document) Image(pageNumber int) (image.Image, error) {
	if pageNumber >= f.NumPage() {
		return nil, ErrPageMissing
	}

	page := C.fz_load_page(f.ctx, f.doc, C.int(pageNumber))
	defer C.fz_drop_page(f.ctx, page)

	var bounds C.fz_rect
	C.fz_bound_page(f.ctx, page, &bounds)

	var ctm C.fz_matrix
	C.fz_scale(&ctm, C.float(300.0/72), C.float(300.0/72))

	var bbox C.fz_irect
	C.fz_transform_rect(&bounds, &ctm)
	C.fz_round_rect(&bbox, &bounds)

	pixmap := C.fz_new_pixmap_with_bbox(f.ctx, C.fz_device_rgb(f.ctx), &bbox, nil, 1)
	if pixmap == nil {
		return nil, ErrCreatePixmap
	}

	C.fz_clear_pixmap_with_value(f.ctx, pixmap, C.int(0xff))
	defer C.fz_drop_pixmap(f.ctx, pixmap)

	device := C.fz_new_draw_device(f.ctx, &ctm, pixmap)
	defer C.fz_drop_device(f.ctx, device)

	draw_matrix := C.fz_identity
	C.fz_run_page(f.ctx, page, device, &draw_matrix, nil)

	pixels := C.fz_pixmap_samples(f.ctx, pixmap)
	if pixels == nil {
		return nil, ErrPixmapSamples
	}

	rect := image.Rect(int(bbox.x0), int(bbox.y0), int(bbox.x1), int(bbox.y1))
	bytes := C.GoBytes(unsafe.Pointer(pixels), C.int(4*bbox.x1*bbox.y1))
	img := &image.RGBA{bytes, 4 * rect.Max.X, rect}

	C.fz_close_device(f.ctx, device)

	return img, nil
}

// Close closes the underlying fitz document.
func (f *Document) Close() error {
	C.fz_drop_document(f.ctx, f.doc)
	C.fz_drop_context(f.ctx)
	return nil
}
