package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Logger writes timestamped lines to both stdout and a log file, in the
// same "YYYY-MM-DDTHH:MM:SSZ [profile] [LEVEL] message" shape the old
// backup_uni.sh log() function used, so existing log-watching habits and
// scripts keep working unchanged.
type Logger struct {
	profile string
	file    *os.File
	mw      io.Writer
}

func NewLogger(workDir, profile, logFileName string) (*Logger, error) {
	logDir := workDir
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(logDir, logFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &Logger{
		profile: profile,
		file:    f,
		mw:      io.MultiWriter(os.Stdout, f),
	}, nil
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

func (l *Logger) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fmt.Fprintf(l.mw, "%s [%s] [%s] %s\n", ts, l.profile, level, msg)
}

func (l *Logger) Info(format string, args ...interface{})  { l.log("INFO", format, args...) }
func (l *Logger) Warn(format string, args ...interface{})  { l.log("WARN", format, args...) }
func (l *Logger) Error(format string, args ...interface{}) { l.log("ERROR", format, args...) }

// fatalLogger wraps log.Fatal-style exits so every failure path writes a
// final line to the log before the process ends, matching the old
// script's trap-based "Falha no perfil ...; código de saída: N" message.
func (l *Logger) Fatal(code int, format string, args ...interface{}) {
	l.Error(format, args...)
	l.Close()
	os.Exit(code)
}

func init() {
	// Avoid Go's default log.* timestamp prefix leaking into plain
	// diagnostic prints used before a Logger exists (e.g. config errors).
	log.SetFlags(0)
}
