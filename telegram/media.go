package telegram

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
)

const (
	DEFAULT_WORKERS = 5
	DEFAULT_PARTS   = 512 * 1024
)

type UploadOptions struct {
	// Worker count for upload file.
	Threads int `json:"threads,omitempty"`
	//  Chunk size for upload file.
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// File name for upload file.
	FileName string `json:"file_name,omitempty"`
}

// UploadFile upload file to telegram.
// file can be string, []byte, io.Reader, fs.File
func (c *Client) UploadFile(file interface{}, Opts ...*UploadOptions) (InputFile, error) {
	opts := getVariadic(Opts, &UploadOptions{}).(*UploadOptions)
	if file == nil {
		return nil, errors.New("file can not be nil")
	}
	u := &Uploader{
		Source:    file,
		Client:    c,
		ChunkSize: opts.ChunkSize,
		Worker:    opts.Threads,
	}
	u.Meta.Name = opts.FileName
	return u.Upload()
}

type (
	Uploader struct {
		*Client
		Parts     int32
		ChunkSize int32
		Worker    int
		Source    interface{}
		Workers   []*Client
		wg        *sync.WaitGroup
		FileID    int64
		Meta      struct {
			Big  bool
			Hash hash.Hash
			Name string
			Size int64
		}
	}
)

func (u *Uploader) Upload() (InputFile, error) {
	u.Init()
	u.Start()
	u.closeWorkers()
	return u.saveFile()
}

func (u *Uploader) Init() error {
	switch s := u.Source.(type) {
	case string:
		if u.Meta.Size == 0 {
			fi, err := os.Stat(s)
			if err != nil {
				return err
			}
			u.Meta.Size = fi.Size()
			u.Meta.Name = fi.Name()
		}
	case []byte:
		u.Meta.Size = int64(len(s))
	case fs.File:
		fi, err := s.Stat()
		if err != nil {
			return err
		}
		u.Meta.Size = fi.Size()
		u.Meta.Name = fi.Name()
	case io.Reader:
		buff := bytes.NewBuffer([]byte{})
		u.Meta.Size, _ = io.Copy(buff, s)
		u.Source = bytes.NewReader(buff.Bytes())
	}
	if u.Parts == 0 {
		u.Parts = int32(u.Meta.Size / DEFAULT_PARTS)
		if u.Parts == 0 {
			u.Parts = 1
		}
	}
	if u.ChunkSize == 0 {
		u.ChunkSize = DEFAULT_PARTS
	}
	if int64(u.ChunkSize) > u.Meta.Size {
		u.ChunkSize = int32(u.Meta.Size)
	}
	if int32(u.Worker) == 0 {
		u.Worker = DEFAULT_WORKERS
	}
	// < 10MB
	if u.Meta.Size < 10*1024*1024 {
		u.Meta.Big = false
		u.Meta.Hash = md5.New()
	} else {
		u.Meta.Big = true
	}
	u.FileID = GenerateRandomLong()
	u.wg = &sync.WaitGroup{}
	return nil
}

func (u *Uploader) allocateWorkers() {
	borrowedSenders, err := u.Client.BorrowExportedSenders(u.Client.GetDC(), u.Worker)
	if err != nil {
		panic(err)
	}
	u.Workers = borrowedSenders

	u.Client.Log.Debug("Allocated workers: ", len(u.Workers))
}

func (u *Uploader) closeWorkers() {}

func (u *Uploader) saveFile() (InputFile, error) {
	if u.Meta.Big {
		return &InputFileBig{u.FileID, u.Parts, u.Meta.Name}, nil
	} else {
		return &InputFileObj{u.FileID, u.Parts, u.Meta.Name, string(u.Meta.Hash.Sum(nil))}, nil
	}
}

func (u *Uploader) DividePartsToWorkers() [][]int32 {
	var (
		parts  = u.Parts
		worker = u.Worker
	)
	if parts < int32(worker) {
		worker = int(parts)
	}
	if int32(worker) == 0 {
		worker = 1
	}
	var (
		perWorker = parts / int32(worker)
		remainder = parts % int32(worker)
	)
	var (
		start = int32(0)
		end   = int32(0)
	)
	var (
		partsToWorkers = make([][]int32, worker)
	)
	for i := 0; i < worker; i++ {
		end = start + perWorker
		if remainder > 0 {
			end++
			remainder--
		}
		partsToWorkers[i] = []int32{start, end}
		start = end
	}
	u.Worker = worker
	u.Parts = parts
	u.allocateWorkers()
	return partsToWorkers
}

func (u *Uploader) Start() error {
	var (
		parts = u.DividePartsToWorkers()
	)
	for i, w := range u.Workers {
		u.wg.Add(1)
		go u.uploadParts(w, parts[i])
	}
	u.wg.Wait()
	return nil
}

func (u *Uploader) readPart(part int32) ([]byte, error) {
	var (
		err error
	)
	switch s := u.Source.(type) {
	case string:
		f, err := os.Open(s)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		_, err = f.Seek(int64(part*u.ChunkSize), 0)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, u.ChunkSize)
		_, err = f.Read(buf)
		if err != nil {
			return nil, err
		}
		return buf, nil
	case []byte:
		return s[part*u.ChunkSize : (part+1)*u.ChunkSize], nil
	case fs.File:
		fs, err := s.Stat()
		if err != nil {
			return nil, err
		}
		f, err := os.Open(fs.Name())
		if err != nil {
			return nil, err
		}
		defer f.Close()
		_, err = f.Seek(int64(part*u.ChunkSize), 0)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, u.ChunkSize)
		_, err = f.Read(buf)
		if err != nil {
			return nil, err
		}
		return buf, nil
	case *bytes.Reader:
		// coverted io.Reader to bytes.Reader
		buf := make([]byte, u.ChunkSize)
		_, err = s.ReadAt(buf, int64(part*u.ChunkSize))
		if err != nil {
			return nil, err
		}
		return buf, nil
	default:
		return nil, errors.New("unknown source type, only support string, []byte, fs.File, io.Reader")
	}
}

func (u *Uploader) uploadParts(w *Client, parts []int32) {
	defer u.wg.Done()
	for i := parts[0]; i < parts[1]; i++ {
		buf, err := u.readPart(i)
		if err != nil {
			u.Client.Log.Error(err)
			continue
		}
		if u.Meta.Big {
			_, err = w.UploadSaveBigFilePart(u.FileID, i, u.Parts, buf)
		} else {
			u.Meta.Hash.Write(buf)
			_, err = w.UploadSaveFilePart(u.FileID, i, buf)
		}
		w.Logger.Debug(fmt.Sprintf("uploaded part %d of %d", i, u.Parts))
		if err != nil {
			panic(err)
		}
	}
}

type DownloadOptions struct {
	// Download path to save file
	FileName string `json:"file_name,omitempty"`
	// Datacenter ID of file
	DcID int32 `json:"dc_id,omitempty"`
	// Size of file
	Size int32 `json:"size,omitempty"`
	// Worker count to download file
	Threads int `json:"threads,omitempty"`
	// Chunk size to download file
	ChunkSize int32 `json:"chunk_size,omitempty"`
}

func (c *Client) DownloadMedia(file interface{}, Opts ...*DownloadOptions) (string, error) {
	opts := getVariadic(Opts, &DownloadOptions{}).(*DownloadOptions)
	location, dc, size, fileName, err := getFileLocation(file)
	if err != nil {
		return "", err
	}
	dc = getValue(dc, opts.DcID).(int32)
	dc = getValue(dc, c.GetDC()).(int32)
	size = getValue(size, int64(opts.Size)).(int64)
	fileName = getValue(opts.FileName, fileName).(string)
	d := &Downloader{
		Client:    c,
		Source:    location,
		FileName:  fileName,
		DcID:      dc,
		Size:      int32(size),
		Worker:    opts.Threads,
		ChunkSize: getValue(opts.ChunkSize, DEFAULT_PARTS).(int32),
	}
	return d.Download()
}

type (
	Downloader struct {
		*Client
		Parts     int32
		ChunkSize int32
		Worker    int
		Source    InputFileLocation
		Size      int32
		DcID      int32
		Workers   []*Client
		FileName  string
		wg        *sync.WaitGroup
	}
)

func (d *Downloader) Download() (string, error) {
	d.Init()
	return d.Start()
}

func (d *Downloader) Init() {
	if d.Parts == 0 {
		d.Parts = int32(d.Size / DEFAULT_PARTS)
		if d.Parts == 0 {
			d.Parts = 1
		}
	}
	if d.ChunkSize == 0 {
		d.ChunkSize = DEFAULT_PARTS
	}

	if d.Worker == 0 {
		d.Worker = DEFAULT_WORKERS
	}
	if d.Worker > int(d.Parts) {
		d.Worker = int(d.Parts)
	}
	d.wg = &sync.WaitGroup{}
	if d.FileName == "" {
		d.FileName = GenerateRandomString(10)
	}
	d.createFile()
	d.allocateWorkers()
}

func (d *Downloader) createFile() (*os.File, error) {
	if pathIsDir(d.FileName) {
		d.FileName = filepath.Join(d.FileName, GenerateRandomString(10))
		os.MkdirAll(filepath.Dir(d.FileName), 0755)
	}
	return os.Create(d.FileName)
}

func (d *Downloader) onError() {
	os.Remove(d.FileName)
}

func (d *Downloader) allocateWorkers() {
	if d.Worker == 1 {
		wNew, err := d.Client.borrowSender(int(d.DcID))
		if err != nil {
			d.Client.Log.Error(err)
		}
		d.Workers = []*Client{wNew}
		return
	}
	wg := &sync.WaitGroup{}
	bs, err := d.Client.BorrowExportedSenders(int(d.DcID), d.Worker)
	if err != nil {
		d.Client.Log.Error(err)
	}
	d.Workers = bs
	wg.Wait()
}

func (d *Downloader) DividePartsToWorkers() [][]int32 {
	var (
		parts  = d.Parts
		worker = d.Worker
	)
	if parts < int32(worker) {
		worker = int(parts)
	}
	var (
		perWorker = parts / int32(worker)
		remainder = parts % int32(worker)
	)
	var (
		start = int32(0)
		end   = int32(0)
	)
	var (
		partsToWorkers = make([][]int32, worker)
	)
	for i := 0; i < worker; i++ {
		end = start + perWorker
		if remainder > 0 {
			end++
			remainder--
		}
		partsToWorkers[i] = []int32{start, end}
		start = end
	}
	d.Worker = worker
	d.Parts = parts
	return partsToWorkers
}

func (d *Downloader) Start() (string, error) {
	var (
		parts = d.DividePartsToWorkers()
	)
	for i, w := range d.Workers {
		d.wg.Add(1)
		go d.downloadParts(w, parts[i])
	}
	d.wg.Wait()
	d.closeWorkers()
	return d.FileName, nil
}

func (d *Downloader) closeWorkers() {} // for now Its Disabled

func (d *Downloader) writeAt(buf []byte, offset int64) error {
	f, err := os.OpenFile(d.FileName, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(buf, offset)
	if err != nil {
		return err
	}
	return nil
}

func (d *Downloader) calcOffset(part int32) int64 {
	return int64(part * d.ChunkSize)
}

func (d *Downloader) downloadParts(w *Client, parts []int32) {
	defer d.wg.Done()
	for i := parts[0]; i < parts[1]; i++ {
		buf, err := w.UploadGetFile(&UploadGetFileParams{
			Location:     d.Source,
			Offset:       d.calcOffset(i),
			Limit:        d.ChunkSize,
			CdnSupported: false,
		})
		if err != nil || buf == nil {
			w.Logger.Warn(err)
			continue
		}
		w.Logger.Debug(fmt.Sprintf("downloaded part %d of %d", i, d.Parts))
		var buffer []byte
		switch v := buf.(type) {
		case *UploadFileObj:
			buffer = v.Bytes
		case *UploadFileCdnRedirect:
			return // TODO
		}
		err = d.writeAt(buffer, d.calcOffset(i))
		if err != nil {
			panic(err)
		}
	}
}

func GenerateRandomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// TODO: IMPLEMENT SenderChat Correctly.
// Add Album Handle and GetMediaGroup Functions.
