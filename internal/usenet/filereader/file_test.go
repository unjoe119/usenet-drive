package filereader

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/javi11/usenet-drive/internal/usenet"
	"github.com/javi11/usenet-drive/internal/usenet/connectionpool"
	"github.com/javi11/usenet-drive/internal/usenet/corruptednzbsmanager"
	"github.com/javi11/usenet-drive/internal/usenet/nzbloader"
	status "github.com/javi11/usenet-drive/internal/usenet/statusreporter"
	"github.com/javi11/usenet-drive/pkg/mmap"
	"github.com/javi11/usenet-drive/pkg/osfs"
	"github.com/stretchr/testify/assert"
)

func TestOpenFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	log := slog.Default()
	mockCNzb := corruptednzbsmanager.NewMockCorruptedNzbsManager(ctrl)
	fs := osfs.NewMockFileSystem(ctrl)
	cp := connectionpool.NewMockUsenetConnectionPool(ctrl)
	mockSr := status.NewMockStatusReporter(ctrl)

	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("Not nzb file", func(t *testing.T) {
		name := "test.txt"
		onClose := func() error { return nil }

		fs.EXPECT().Stat("test.nzb").Return(nil, os.ErrNotExist).Times(1)
		fs.EXPECT().IsNotExist(os.ErrNotExist).Return(true).Times(1)

		_, f, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 1,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)
		t.Cleanup(func() {
			if f != nil {
				f.Close()
			}
		})
		assert.NoError(t, err)
	})

	t.Run("Is a Nzb file but do not exists", func(t *testing.T) {
		name := "test.nzb"
		onClose := func() error { return nil }

		fs.EXPECT().Stat(name).Return(nil, os.ErrNotExist).Times(1)

		_, f, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 1,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)
		t.Cleanup(func() {
			if f != nil {
				f.Close()
			}
		})
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Is a Nzb file", func(t *testing.T) {
		name := "test.mkv.nzb"
		onClose := func() error { return nil }
		fsStatMock := osfs.NewMockFileInfo(ctrl)

		f, err := os.Open("../../test/nzbmock.xml")
		assert.NoError(t, err)
		st, err := f.Stat()
		assert.NoError(t, err)

		fsStatMock.EXPECT().Size().Return(st.Size()).Times(1)
		assert.NoError(t, err)
		fs.EXPECT().Open(name).Return(f, nil).Times(1)
		fs.EXPECT().Stat(name).Return(fsStatMock, nil).Times(1)

		mockSr.EXPECT().StartDownload(gomock.Any(), name).Times(1)

		// Call
		ok, file, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 0,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "test.mkv.bin", file.Name())
	})

	t.Run("Is a Nzb file masked", func(t *testing.T) {
		name := "test.mkv.bin"
		onClose := func() error { return nil }

		fsStatMock := osfs.NewMockFileInfo(ctrl)
		fsStatMock.EXPECT().Name().Return("test.mkv.nzb").Times(1)

		fs.EXPECT().Stat("test.mkv.nzb").Return(fsStatMock, nil).Times(1)
		fs.EXPECT().IsNotExist(nil).Return(false).Times(1)
		mockSr.EXPECT().StartDownload(gomock.Any(), "test.mkv.nzb").Times(1)

		f, err := os.Open("../../test/nzbmock.xml")
		assert.NoError(t, err)
		st, err := f.Stat()
		assert.NoError(t, err)

		fsStatMock.EXPECT().Size().Return(st.Size()).Times(1)

		fs.EXPECT().Open("test.mkv.nzb").Return(f, nil).Times(1)

		// Call
		ok, file, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 1,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)

		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "test.mkv.bin", file.Name())
	})

	t.Run("Nzb file with corrupted metadata", func(t *testing.T) {
		name := "test.nzb"
		onClose := func() error { return nil }

		fsStatMock := osfs.NewMockFileInfo(ctrl)

		fs.EXPECT().Stat("test.nzb").Return(fsStatMock, nil).Times(1)

		f, err := os.Open("../../test/corruptednzbmock.xml")
		assert.NoError(t, err)
		st, err := f.Stat()
		assert.NoError(t, err)

		fsStatMock.EXPECT().Size().Return(st.Size()).Times(1)

		fs.EXPECT().Open("test.nzb").Return(f, nil).Times(1)
		mockCNzb.EXPECT().Add(context.Background(), "test.nzb", "corrupted nzb file, missing required metadata").Return(nil).Times(1)

		ok, file, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 1,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)

		t.Cleanup(func() {
			if file != nil {
				file.Close()
			}
		})
		assert.ErrorIs(t, err, os.ErrNotExist)
		// File exists but is corrupted
		assert.True(t, ok)
	})

	t.Run("Error opening the file", func(t *testing.T) {
		name := "test.nzb"
		onClose := func() error { return nil }

		fs.EXPECT().Open("test.nzb").Return(nil, os.ErrPermission).Times(1)
		fsStatMock := osfs.NewMockFileInfo(ctrl)

		fs.EXPECT().Stat("test.nzb").Return(fsStatMock, nil).Times(1)

		ok, file, err := openFile(
			context.Background(),
			name,
			cp,
			log,
			onClose,
			mockCNzb,
			fs,
			downloadConfig{
				maxDownloadRetries: 5,
				maxDownloadWorkers: 1,
				maxBufferSizeInMb:  30,
			},
			mockSr,
		)

		t.Cleanup(func() {
			if file != nil {
				file.Close()
			}
		})

		assert.ErrorIs(t, err, os.ErrPermission)
		// File should be an nzb at this point but we cannot open it
		assert.True(t, ok)
	})

}

func TestCloseFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	log := slog.Default()
	mockCNzb := corruptednzbsmanager.NewMockCorruptedNzbsManager(ctrl)
	fs := osfs.NewMockFileSystem(ctrl)
	mockBuffer := NewMockBuffer(ctrl)
	nzbReader := nzbloader.NewMockNzbReader(ctrl)

	onClosedCalled := false
	mockSr := status.NewMockStatusReporter(ctrl)

	mmapFile := mmap.NewMockMmapFileData(ctrl)

	t.Run("Error", func(t *testing.T) {
		f := &file{
			path:      "test.nzb",
			buffer:    mockBuffer,
			mmapFile:  mmapFile,
			fsMutex:   sync.RWMutex{},
			log:       log,
			metadata:  usenet.Metadata{},
			nzbReader: nzbReader,
			onClose: func() error {
				onClosedCalled = true
				return nil
			},
			cNzb: mockCNzb,
			fs:   fs,
			sr:   mockSr,
		}
		nzbReader.EXPECT().Close().Return().Times(1)
		mmapFile.EXPECT().Close().Return(os.ErrPermission).Times(1)
		mockBuffer.EXPECT().Close().Return(nil).Times(1)
		mockSr.EXPECT().FinishDownload(gomock.Any()).Times(1)

		err := f.Close()
		assert.ErrorIs(t, err, os.ErrPermission)

		assert.False(t, onClosedCalled)
	})

	t.Run("Success", func(t *testing.T) {
		f := &file{
			path:      "test.nzb",
			buffer:    mockBuffer,
			mmapFile:  mmapFile,
			fsMutex:   sync.RWMutex{},
			log:       log,
			metadata:  usenet.Metadata{},
			nzbReader: nzbReader,
			onClose: func() error {
				onClosedCalled = true
				return nil
			},
			cNzb: mockCNzb,
			fs:   fs,
			sr:   mockSr,
		}
		nzbReader.EXPECT().Close().Return().Times(1)
		mmapFile.EXPECT().Close().Return(nil).Times(1)
		mockBuffer.EXPECT().Close().Return(nil).Times(1)
		mockSr.EXPECT().FinishDownload(gomock.Any()).Times(1)

		err := f.Close()
		assert.NoError(t, err)

		assert.True(t, onClosedCalled)
	})

	t.Run("NoOnCloseFunction", func(t *testing.T) {
		f := &file{
			path:      "test.nzb",
			buffer:    mockBuffer,
			mmapFile:  mmapFile,
			fsMutex:   sync.RWMutex{},
			log:       log,
			metadata:  usenet.Metadata{},
			nzbReader: nzbReader,
			onClose: func() error {
				onClosedCalled = true
				return nil
			},
			cNzb: mockCNzb,
			fs:   fs,
			sr:   mockSr,
		}
		f.onClose = nil
		nzbReader.EXPECT().Close().Return().Times(1)
		mmapFile.EXPECT().Close().Return(nil).Times(1)
		mockBuffer.EXPECT().Close().Return(nil).Times(1)
		mockSr.EXPECT().FinishDownload(gomock.Any()).Times(1)

		err := f.Close()
		assert.NoError(t, err)
	})

}

func TestRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	log := slog.Default()
	mockCNzb := corruptednzbsmanager.NewMockCorruptedNzbsManager(ctrl)
	fs := osfs.NewMockFileSystem(ctrl)
	mockBuffer := NewMockBuffer(ctrl)
	mockSr := status.NewMockStatusReporter(ctrl)
	mmapFile := mmap.NewMockMmapFileData(ctrl)

	t.Run("Read success", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		b := []byte("test")
		n := len(b)

		mockSr.EXPECT().AddTimeData(gomock.Any(), gomock.Any()).Times(1)
		mockBuffer.EXPECT().Read(b).Return(n, nil)

		n2, err := f.Read(b)
		assert.NoError(t, err)
		assert.Equal(t, n, n2)
	})

	t.Run("Mark file as corrupted on read error", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		b := []byte("test")
		n := len(b)

		mockBuffer.EXPECT().Read(b).Return(n, ErrCorruptedNzb)
		mockCNzb.EXPECT().Add(context.Background(), "test.nzb", "corrupted nzb").Return(nil)

		n2, err := f.Read(b)
		assert.Equal(t, n, n2)
		assert.Equal(t, io.ErrUnexpectedEOF, err)
	})
}

func TestReadAt(t *testing.T) {
	ctrl := gomock.NewController(t)
	log := slog.Default()
	mockCNzb := corruptednzbsmanager.NewMockCorruptedNzbsManager(ctrl)
	fs := osfs.NewMockFileSystem(ctrl)
	mockBuffer := NewMockBuffer(ctrl)
	mockSr := status.NewMockStatusReporter(ctrl)
	mmapFile := mmap.NewMockMmapFileData(ctrl)

	t.Run("ReadAt success", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		b := []byte("test")
		n := len(b)
		offset := int64(10)

		mockBuffer.EXPECT().ReadAt(b, offset).Return(n, nil)

		n2, err := f.ReadAt(b, offset)
		assert.NoError(t, err)
		assert.Equal(t, n, n2)
	})

	t.Run("Mark file as corrupted on read at error", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		b := []byte("test")
		n := len(b)
		offset := int64(10)

		mockBuffer.EXPECT().ReadAt(b, offset).Return(n, ErrCorruptedNzb)
		mockCNzb.EXPECT().Add(context.Background(), "test.nzb", "corrupted nzb").Return(nil)

		n2, err := f.ReadAt(b, offset)
		assert.Equal(t, n, n2)
		assert.Equal(t, io.ErrUnexpectedEOF, err)
	})
}

func TestSystemFileMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	log := slog.Default()
	mockCNzb := corruptednzbsmanager.NewMockCorruptedNzbsManager(ctrl)
	fs := osfs.NewMockFileSystem(ctrl)
	mockFile := osfs.NewMockFile(ctrl)
	mockBuffer := NewMockBuffer(ctrl)
	mockSr := status.NewMockStatusReporter(ctrl)
	mmapFile := mmap.NewMockMmapFileData(ctrl)

	t.Run("Chown", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}
		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Chown(1000, 1000).Return(nil)

		uid, gid := 1000, 1000
		err := f.Chown(uid, gid)
		assert.NoError(t, err)
	})

	t.Run("Chdir", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}
		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Chdir().Return(nil)

		err := f.Chdir()
		assert.NoError(t, err)
	})

	t.Run("Chmod", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}
		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Chmod(os.FileMode(0644)).Return(nil)

		mode := os.FileMode(0644)
		err := f.Chmod(mode)
		assert.NoError(t, err)
	})

	t.Run("Fd", func(t *testing.T) {
		fd := uintptr(123)
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Fd().Return(fd)

		assert.Equal(t, fd, f.Fd())
	})

	t.Run("Name", func(t *testing.T) {
		name := "test.nzb"
		f := &file{
			path:     name,
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
		}

		assert.Equal(t, name, f.Name())
	})

	t.Run("Readdirnames", func(t *testing.T) {
		names := []string{"file1", "file2"}
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Readdirnames(0).Return(names, nil)

		names2, err := f.Readdirnames(0)
		assert.NoError(t, err)
		assert.Equal(t, names, names2)
	})

	t.Run("SetDeadline", func(t *testing.T) {
		tm := time.Now()
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().SetDeadline(tm).Return(nil)

		err := f.SetDeadline(tm)
		assert.NoError(t, err)
	})

	t.Run("SetReadDeadline", func(t *testing.T) {
		tm := time.Now()
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().SetReadDeadline(tm).Return(nil)

		err := f.SetReadDeadline(tm)
		assert.NoError(t, err)
	})

	t.Run("SetWriteDeadline", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		err := f.SetWriteDeadline(time.Now())
		assert.Equal(t, os.ErrPermission, err)
	})

	t.Run("Sync", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
		}

		mmapFile.EXPECT().File().Return(mockFile).Times(1)
		mockFile.EXPECT().Sync().Return(nil)

		err := f.Sync()
		assert.NoError(t, err)
	})

	t.Run("Truncate", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		err := f.Truncate(123)
		assert.Equal(t, os.ErrPermission, err)
	})

	t.Run("Write", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		n, err := f.Write([]byte("test"))
		assert.Equal(t, 0, n)
		assert.Equal(t, os.ErrPermission, err)
	})

	t.Run("WriteAt", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		n, err := f.WriteAt([]byte("test"), 0)
		assert.Equal(t, 0, n)
		assert.Equal(t, os.ErrPermission, err)
	})

	t.Run("WriteString", func(t *testing.T) {
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		n, err := f.WriteString("test")
		assert.Equal(t, 0, n)
		assert.Equal(t, os.ErrPermission, err)
	})

	t.Run("Seek", func(t *testing.T) {
		offset := int64(0)
		whence := io.SeekStart
		n := int64(123)
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{},
			onClose:  func() error { return nil },
			cNzb:     mockCNzb,
			fs:       fs,
			sr:       mockSr,
		}

		mockBuffer.EXPECT().Seek(offset, whence).Return(n, nil)

		n2, err := f.Seek(offset, whence)
		assert.NoError(t, err)
		assert.Equal(t, n, n2)
	})

	t.Run("Stat", func(t *testing.T) {
		today := time.Now()
		f := &file{
			path:     "test.nzb",
			buffer:   mockBuffer,
			mmapFile: mmapFile,
			fsMutex:  sync.RWMutex{},
			log:      log,
			metadata: usenet.Metadata{
				FileExtension: ".mkv",
				FileSize:      123,
				ChunkSize:     456,
				FileName:      "test.mkv",
				ModTime:       today,
			},
			onClose: func() error { return nil },
			cNzb:    mockCNzb,
			fs:      fs,
			sr:      mockSr,
		}

		mockFsStat := osfs.NewMockFileInfo(ctrl)
		mockFsStat.EXPECT().Name().Return("test.nzb").Times(1)

		mmapFile.EXPECT().File().Return(mockFile).Times(1)

		mockFile.EXPECT().Name().Return("folder/test.nzb").Times(1)
		fs.EXPECT().Stat("folder/test.nzb").Return(mockFsStat, nil).Times(1)

		info, err := f.Stat()
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "test.mkv", info.Name())
		assert.Equal(t, int64(123), info.Size())
		assert.Equal(t, today, info.ModTime())
	})
}
