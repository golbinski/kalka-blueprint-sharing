package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"time"
)

func writeTarGz(w io.Writer, files []file, mtime time.Time) error {
	gz, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	gz.ModTime = time.Time{} // no timestamp or name in the gzip header
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), ModTime: mtime, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(f.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZip(w io.Writer, files []file, mtime time.Time) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(os.FileMode(f.mode))
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	return zw.Close()
}
