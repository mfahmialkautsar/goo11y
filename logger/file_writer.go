package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultFileWriterBuffer = 1024
	fileWriterDirMode       = 0o755
	fileWriterFileMode      = 0o644
)

type dailyFileWriter struct {
	directory string
	queue     chan []byte
	now       func() time.Time

	mu          sync.Mutex
	currentDate string
	file        *os.File
}

func newDailyFileWriter(cfg FileConfig) (*dailyFileWriter, error) {
	if cfg.Directory == "" {
		return nil, fmt.Errorf("missing file log directory")
	}

	buffer := cfg.Buffer
	if buffer <= 0 {
		buffer = defaultFileWriterBuffer
	}

	w := &dailyFileWriter{
		directory: cfg.Directory,
		queue:     make(chan []byte, buffer),
		now:       time.Now,
	}

	go w.run()

	return w, nil
}

func (w *dailyFileWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	copyBuf := make([]byte, len(p))
	copy(copyBuf, p)

	w.queue <- copyBuf

	return len(p), nil
}

func (w *dailyFileWriter) run() {
	for payload := range w.queue {
		if err := w.write(payload); err != nil {
			fmt.Fprintf(os.Stderr, "goo11y logger file writer error: %v\n", err)
		}
	}
}

func (w *dailyFileWriter) write(payload []byte) error {
	now := w.now()
	currentDate := now.Format("2006-01-02")

	if err := w.ensureFile(currentDate); err != nil {
		return err
	}

	w.mu.Lock()
	file := w.file
	w.mu.Unlock()

	if file == nil {
		return fmt.Errorf("file handle unavailable")
	}

	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write log file: %w", err)
	}

	return nil
}

func (w *dailyFileWriter) ensureFile(date string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentDate == date && w.file != nil {
		return nil
	}

	if err := os.MkdirAll(w.directory, fileWriterDirMode); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	path := filepath.Join(w.directory, date+".log")

	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileWriterFileMode)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	w.file = file
	w.currentDate = date

	return nil
}
