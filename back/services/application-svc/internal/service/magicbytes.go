package service

import (
	"bytes"
	"errors"
	"io"
)

var ErrUnsupportedFile = errors.New("unsupported file type")

// DetectFileKind checks file signatures without consuming the upload stream.
// The declared Content-Type is not trusted.
func DetectFileKind(r io.Reader) (mime, ext string, restored io.Reader, err error) {
	head := make([]byte, 512)
	n, _ := io.ReadFull(r, head)
	head = head[:n]

	switch {
	case bytes.HasPrefix(head, []byte("%PDF-")):
		mime, ext = "application/pdf", ".pdf"
	case bytes.HasPrefix(head, []byte{0x50, 0x4B, 0x03, 0x04}):
		// ZIP signature — DOCX/XLSX. Distinguish by Office path literals.
		if bytes.Contains(head, []byte("word/")) {
			mime, ext = "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"
		} else if bytes.Contains(head, []byte("xl/")) {
			mime, ext = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"
		} else {
			return "", "", nil, ErrUnsupportedFile
		}
	case bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		mime, ext = "image/png", ".png"
	default:
		return "", "", nil, ErrUnsupportedFile
	}
	return mime, ext, io.MultiReader(bytes.NewReader(head), r), nil
}
