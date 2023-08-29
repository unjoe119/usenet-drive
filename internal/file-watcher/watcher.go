package filewatcher

import (
	"context"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	uploadqueue "github.com/javi11/usenet-drive/internal/upload-queue"
)

type Watcher struct {
	watcher       *fsnotify.Watcher
	queue         uploadqueue.UploadQueue
	log           *log.Logger
	fileWhitelist []string
}

func NewWatcher(queue uploadqueue.UploadQueue, log *log.Logger, fileWhitelist []string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		watcher:       watcher,
		queue:         queue,
		log:           log,
		fileWhitelist: fileWhitelist,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) {
	w.log.Printf("Starting file watcher...")

	go func() {
		var (
			// Wait 10s for new events; each new event resets the timer.
			waitFor = 10000 * time.Millisecond

			// Keep track of the timers, as path → timer.
			mu     sync.Mutex
			timers = make(map[string]*time.Timer)

			// Callback we run.
			addToQueue = func(e fsnotify.Event) {
				// Don't need to remove the timer if you don't have a lot of files.
				mu.Lock()
				delete(timers, e.Name)
				mu.Unlock()
				w.log.Printf("File %s created, adding to upload queue", e.Name)
				w.queue.AddJob(ctx, e.Name)
			}
		)

		for {
			select {
			case <-ctx.Done():
				return
			// Read from Errors.
			case err, ok := <-w.watcher.Errors:
				if !ok { // Channel was closed (i.e. Watcher.Close() was called).
					return
				}
				if err != nil {
					w.log.Printf("file watcher error: %v", err)
					return
				}
			// Read from Events.
			case e, ok := <-w.watcher.Events:
				if !ok { // Channel was closed (i.e. Watcher.Close() was called).
					return
				}

				// We just want to watch for file creation, so ignore everything
				// outside of Create and Write and files that don't have an allowed extension.
				if (e.Has(fsnotify.Create) || e.Has(fsnotify.Write)) && hasAllowedExtension(e.Name, w.fileWhitelist) {
					// Get timer.
					mu.Lock()
					t, ok := timers[e.Name]
					mu.Unlock()

					// No timer yet, so create one.
					if !ok {
						t = time.AfterFunc(math.MaxInt64, func() { addToQueue(e) })
						t.Stop()

						mu.Lock()
						timers[e.Name] = t
						mu.Unlock()
					}

					// Reset the timer for this path, so it will start from 100ms again.
					t.Reset(waitFor)
				}
			}
		}
	}()
}

func (w *Watcher) Add(path string) error {
	w.log.Printf("Adding %s to file watcher", path)
	return filepath.Walk(path, w.watchDir)
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}

func (w *Watcher) watchDir(path string, fi os.FileInfo, err error) error {

	// since fsnotify can watch all the files in a directory, watchers only need
	// to be added to each nested directory
	if fi.Mode().IsDir() {
		return w.watcher.Add(path)
	}

	return nil
}

func hasAllowedExtension(path string, extensions []string) bool {
	for _, ext := range extensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}