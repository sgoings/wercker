package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

var (
	// ErrEmptyTarball is returned when the tarball has no files in it
	ErrEmptyTarball = errors.New("empty tarball")
)

// ArchiveProcessor is a stream processor for the archive tarballs
type ArchiveProcessor interface {
	Process(*tar.Header, io.Reader) (*tar.Header, io.Reader, error)
}

// Archive holds the tarball stream and provides methods to manipulate it
type Archive struct {
	stream io.Reader
}

// NewArchive constructor
func NewArchive(stream io.Reader) *Archive {
	return &Archive{stream: stream}
}

// Stream is the low-level interface to the archive stream processor
func (a *Archive) Stream(processors ...ArchiveProcessor) error {
	tarball := tar.NewReader(a.stream)
	var tarfile io.Reader
EntryLoop:
	for {
		hdr, err := tarball.Next()
		if err == io.EOF {
			// finished the tar
			break
		}
		// basic filter, we never care about this entry
		if hdr.Name == "./" {
			continue EntryLoop
		}

		tarfile = tarball
		for _, processor := range processors {
			hdr, tarfile, err = processor.Process(hdr, tarfile)
			if err != nil {
				return err
			}
			// if hdr is nil, skip this file
			if hdr == nil {
				continue EntryLoop
			}
		}
	}
	return nil
}

// Single file extraction with max size and empty check
func (a *Archive) Single(source, target string, maxSize int64) chan error {
	single := &ArchiveSingle{Name: source}
	empty := &ArchiveCheckEmpty{}
	max := &ArchiveMaxSize{MaxSize: maxSize}
	extract := &ArchiveExtract{}
	defer extract.Clean()

	errs := make(chan error)
	go func() {
		defer close(errs)
		err := a.Stream(
			single,
			empty,
			max,
			extract,
		)
		if err != nil {
			errs <- err
			return
		}
		if empty.IsEmpty() {
			errs <- ErrEmptyTarball
			return
		}
		errs <- nil
	}()
	return errs
}

// Multi file extraction with max size and empty check
func (a *Archive) Multi(source, target string, maxSize int64) chan error {
	empty := &ArchiveCheckEmpty{}
	max := &ArchiveMaxSize{MaxSize: maxSize}
	extract := &ArchiveExtract{}
	defer extract.Clean()

	errs := make(chan error)
	go func() {
		defer close(errs)
		err := a.Stream(
			empty,
			max,
			extract,
		)
		if err != nil {
			errs <- err
			return
		}
		if empty.IsEmpty() {
			errs <- ErrEmptyTarball
			return
		}
		extract.Rename(source, target)
		errs <- nil
	}()
	return errs
}

// SingleBytes gives you the bytes of a single file, with empty check
func (a *Archive) SingleBytes(source string, dst *bytes.Buffer) chan error {
	single := &ArchiveSingle{Name: source}
	empty := &ArchiveCheckEmpty{}
	buffer := &ArchiveBytes{dst}

	errs := make(chan error)
	go func() {
		defer close(errs)
		err := a.Stream(
			single,
			empty,
			buffer,
		)
		if err != nil {
			errs <- err
			return
		}
		if empty.IsEmpty() {
			errs <- ErrEmptyTarball
			return
		}
		errs <- nil
	}()
	return errs
}

// ArchiveCheckEmpty is an ArchiveProcessor to check whether a stream is empty
type ArchiveCheckEmpty struct {
	hasFiles bool
}

// Process impl
func (p *ArchiveCheckEmpty) Process(hdr *tar.Header, r io.Reader) (*tar.Header, io.Reader, error) {
	if p.hasFiles {
		return hdr, r, nil
	}
	if !hdr.FileInfo().IsDir() {
		p.hasFiles = true
	}
	return hdr, r, nil
}

// IsEmpty will represent whether the tarball was empty after processing
func (p *ArchiveCheckEmpty) IsEmpty() bool {
	return !p.hasFiles
}

// ArchiveMaxSize throws an error and stop stream if MaxSize reached
type ArchiveMaxSize struct {
	MaxSize     int64 // in bytes
	currentSize int64 // in bytes
}

// Process impl
func (p *ArchiveMaxSize) Process(hdr *tar.Header, r io.Reader) (*tar.Header, io.Reader, error) {
	// Check max size
	p.currentSize += hdr.Size
	if p.currentSize >= p.MaxSize {
		err := fmt.Errorf("Size exceeds maximum size of %dMB", p.MaxSize/(1024*1024))
		return hdr, r, err
	}
	return hdr, r, nil
}

// Extract everything to a tempdir, provide methods for Commit and Cleanup
type ArchiveExtract struct {
	// Target  string // target path
	// Source  string // path within the tarball
	tempDir string // path where temporary extraction occurs
}

// Process impl
func (p *ArchiveExtract) Process(hdr *tar.Header, r io.Reader) (*tar.Header, io.Reader, error) {
	if p.tempDir == "" {
		t, err := ioutil.TempDir("", "tar-")
		if err != nil {
			return hdr, r, err
		}
		p.tempDir = t
	}

	// If a directory make it and continue
	fpath := filepath.Join(p.tempDir, hdr.Name)
	if hdr.FileInfo().IsDir() {
		err := os.MkdirAll(fpath, 0755)
		return hdr, r, err
	}

	// Extract the file!
	file, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, hdr.FileInfo().Mode())
	if err != nil {
		return hdr, r, err
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	if err != nil {
		return hdr, r, err
	}

	return hdr, r, nil
}

// TempDir is where we temporarily extracted the file, make sure to delete it
// with Clean()
func (p *ArchiveExtract) TempDir() string {
	return p.tempDir
}

// Rename one of the extracted paths to the target path
func (p *ArchiveExtract) Rename(source, target string) error {
	return os.Rename(filepath.Join(p.tempDir, source), target)
}

// Clean should be called to clean up the tempdir
func (p *ArchiveExtract) Clean() {
	if p.tempDir != "" {
		os.RemoveAll(p.tempDir)
	}
}

// ArchiveSingle filters all but a single item out of the string
type ArchiveSingle struct {
	Name string
}

// Process impl
func (p *ArchiveSingle) Process(hdr *tar.Header, r io.Reader) (*tar.Header, io.Reader, error) {
	if hdr.Name == p.Name {
		return hdr, r, nil
	}
	return nil, r, nil
}

// ArchiveBytes is expected to be used with an ArchiveSingle filter so that it
// only gets one file, if not the buffer will be pretty silly
type ArchiveBytes struct {
	*bytes.Buffer
}

// Proceses writes the bytes for a file to ourselves (a bytes.Buffer)
func (p *ArchiveBytes) Process(hdr *tar.Header, r io.Reader) (*tar.Header, io.Reader, error) {
	_, err := io.Copy(p, r)
	return hdr, r, err
}
