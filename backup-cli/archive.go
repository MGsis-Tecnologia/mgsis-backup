package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// compressToTarGz packages a single file into a .tar.gz, streaming
// through the file rather than loading it into memory — needed since
// pg_dump output here routinely runs several hundred MB. Replaces
// `tar -czf` from the shell version with no external binary.
func compressToTarGz(srcPath, destPath string) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("não consegui ler informações de %s: %w", srcPath, err)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	gz := gzip.NewWriter(destFile)
	tw := tar.NewWriter(gz)

	header := &tar.Header{
		Name:    baseName(srcPath),
		Size:    srcInfo.Size(),
		Mode:    0600,
		ModTime: srcInfo.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("falha ao escrever cabeçalho do tar: %w", err)
	}
	if _, err := io.Copy(tw, srcFile); err != nil {
		return fmt.Errorf("falha ao compactar: %w", err)
	}

	// Close in dependency order and check every error — a silently
	// truncated .tar.gz is exactly the kind of corrupt backup the whole
	// verification system exists to catch, so don't let a Close() error
	// here go unnoticed.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("falha ao finalizar tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("falha ao finalizar gzip: %w", err)
	}
	return destFile.Close()
}

// validateTarGz fully reads back a .tar.gz, decompressing and walking
// every entry, equivalent to `tar -tzf` — it catches truncation or
// corruption before the file is ever uploaded.
func validateTarGz(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip inválido: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar corrompido: %w", err)
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("falha ao ler conteúdo de %s dentro do tar: %w", hdr.Name, err)
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("arquivo tar.gz não contém nenhum arquivo")
	}
	return nil
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
