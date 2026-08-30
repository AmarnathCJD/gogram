package telegram

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAlignedFetchRangeIncludesLeadingSkip(t *testing.T) {
	t.Parallel()

	aligned, skip, limit := alignedFetchRange(1, 4097, 1<<20)
	if aligned != 0 || skip != 1 || limit != 8192 {
		t.Fatalf("alignedFetchRange(1,4097) = (%d,%d,%d), want (0,1,8192)", aligned, skip, limit)
	}

	// An unaligned full MiB needs a second wire fetch because a single Telegram
	// request cannot carry the skipped leading byte plus the requested MiB.
	aligned, skip, limit = alignedFetchRange(1, 1+(1<<20), 1<<20)
	if aligned != 0 || skip != 1 || limit != 1<<20 {
		t.Fatalf("alignedFetchRange(full MiB) = (%d,%d,%d), want (0,1,%d)", aligned, skip, limit, 1<<20)
	}
}

func TestTrimChunkExactRequestedWindow(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789")
	got, eof := trimChunk(data, 2, 10, 5)
	if string(got) != "23456" {
		t.Fatalf("trimChunk = %q, want %q", got, "23456")
	}
	if !eof { // output was clipped to remaining, so this request is complete.
		t.Fatal("trimChunk eof = false, want true after remaining-byte clip")
	}
}

func TestVerifyCDNPart(t *testing.T) {
	t.Parallel()
	data := []byte("verified CDN bytes")
	digest := sha256.Sum256(data)
	cdn := &cdnRedirect{hashes: map[int64]*FileHash{
		0: {Offset: 0, Limit: int32(len(data)), Hash: digest[:]},
	}}
	job := &downloadJob{}
	if err := job.verifyCDNPart(context.Background(), cdn, 0, data); err != nil {
		t.Fatalf("verifyCDNPart(valid) error = %v", err)
	}

	cdn.hashes[0] = &FileHash{Offset: 0, Limit: int32(len(data)), Hash: make([]byte, sha256.Size)}
	if err := job.verifyCDNPart(context.Background(), cdn, 0, data); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("verifyCDNPart(mismatch) error = %v, want hash mismatch", err)
	}
}

func TestVerifyCDNPartVerifiesEverySegment(t *testing.T) {
	t.Parallel()
	data := []byte("abcdef")
	first := sha256.Sum256(data[:3])
	second := sha256.Sum256(data[3:])
	cdn := &cdnRedirect{hashes: map[int64]*FileHash{
		0: {Offset: 0, Limit: 3, Hash: first[:]},
		3: {Offset: 3, Limit: 3, Hash: second[:]},
	}}
	if err := (&downloadJob{}).verifyCDNPart(context.Background(), cdn, 0, data); err != nil {
		t.Fatalf("verifyCDNPart(multi-segment) error = %v", err)
	}
}

func TestVerifyCDNPartRejectsUncoveredBytes(t *testing.T) {
	t.Parallel()
	job := &downloadJob{}
	cdn := &cdnRedirect{hashes: map[int64]*FileHash{}}
	// No client is needed: the offset is deliberately not hash-covered, so the
	// verifier must not return data as valid.
	if err := job.verifyCDNPart(context.Background(), cdn, 7, []byte("x")); err == nil {
		t.Fatal("verifyCDNPart accepted uncovered data")
	}
}

func TestDecryptCDNBlockRejectsInvalidMaterial(t *testing.T) {
	t.Parallel()
	if err := decryptCDNBlock([]byte("x"), []byte("bad"), make([]byte, aes.BlockSize), 0); err == nil {
		t.Fatal("decryptCDNBlock accepted invalid AES key")
	}
	if err := decryptCDNBlock([]byte("x"), make([]byte, 32), []byte("short"), 0); err == nil {
		t.Fatal("decryptCDNBlock accepted invalid IV")
	}
}

func TestDownloadRangeAlignedRejectsInvalidRange(t *testing.T) {
	t.Parallel()
	var c Client
	if _, _, err := c.downloadRangeAligned(nil, nil, 10, 10, 4096, "x"); err == nil {
		t.Fatal("downloadRangeAligned accepted empty/invalid range")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestIsFileReferenceError(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{"FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID", "FILE_REFERENCE_EMPTY"} {
		if !isFileReferenceError(errors.New(msg)) {
			t.Errorf("isFileReferenceError(%q) = false, want true", msg)
		}
	}
	if isFileReferenceError(errors.New("LOCATION_INVALID")) {
		t.Error("LOCATION_INVALID must not be treated as a refreshable file-reference error")
	}
}

func TestDownloadDestinationRejectsSilentShortWrite(t *testing.T) {
	t.Parallel()
	d := &downloadDestination{writer: shortWriter{}}
	n, err := d.WriteAt([]byte("short"), 0)
	if n != 4 {
		t.Fatalf("WriteAt wrote %d bytes, want 4", n)
	}
	if err != io.ErrShortWrite {
		t.Fatalf("WriteAt error = %v, want io.ErrShortWrite", err)
	}
}

func TestDecryptCDNBlockRoundTrip(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 32)
	iv := bytes.Repeat([]byte{3}, aes.BlockSize)
	plain := []byte("round-trip CDN bytes")
	ciphertext := append([]byte(nil), plain...)
	if err := decryptCDNBlock(ciphertext, key, iv, 0); err != nil {
		t.Fatalf("first CTR transform: %v", err)
	}
	if err := decryptCDNBlock(ciphertext, key, iv, 0); err != nil {
		t.Fatalf("second CTR transform: %v", err)
	}
	if !bytes.Equal(ciphertext, plain) {
		t.Fatalf("CTR round trip = %q, want %q", ciphertext, plain)
	}
}
