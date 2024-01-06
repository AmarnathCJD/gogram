// Copyright (c) 2024 RoseLoverX

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
	"strings"
	"sync"

	"github.com/pkg/errors"
)

const (
	DEFAULT_WORKERS = 4
	DEFAULT_PARTS   = 512 * 512
)

type UploadOptions struct {
	// Worker count for upload file.
	Threads int `json:"threads,omitempty"`
	//  Chunk size for upload file.
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// File name for upload file.
	FileName string `json:"file_name,omitempty"`
	// output Callback for upload progress, total parts and uploaded parts.
	ProgressCallback func(totalParts int32, uploadedParts int32) `json:"-"`
}

type FileMeta struct {
	FileName string    `json:"file_name,omitempty"`
	FileSize int64     `json:"file_size,omitempty"`
	IsBig    bool      `json:"is_big,omitempty"`
	Md5Hash  hash.Hash `json:"md_5_hash,omitempty"`
	OpenFile *os.File  `json:"open_file,omitempty"`
}

type Uploader struct {
	*Client
	Parts     int32       `json:"parts,omitempty"`
	ChunkSize int32       `json:"chunk_size,omitempty"`
	Worker    int         `json:"worker,omitempty"`
	Source    interface{} `json:"source,omitempty"`
	Workers   []*Client   `json:"workers,omitempty"`
	wg        *sync.WaitGroup
	FileID    int64 `json:"file_id,omitempty"`
	progress  func(int32, int32)
	totalDone int32
	Meta      FileMeta `json:"meta,omitempty"`
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
		Meta: FileMeta{
			FileName: opts.FileName,
		},
	}
	if opts.ProgressCallback != nil {
		u.progress = opts.ProgressCallback
	}
	return u.Upload()
}

func (u *Uploader) Upload() (InputFile, error) {
	if err := u.Init(); err != nil {
		return nil, err
	}
	if err := u.Start(); err != nil {
		return nil, err
	}
	if u.progress != nil {
		u.progress(u.Parts, u.Parts)
	}
	return u.saveFile(), nil
}

func (u *Uploader) Init() error {
	switch s := u.Source.(type) {
	case string:
		if u.Meta.FileSize == 0 {
			fi, err := os.Stat(s)
			if err != nil {
				return errors.Wrap(err, "can not get file size")
			}
			u.Meta.FileSize = fi.Size()
			u.Meta.FileName = fi.Name()
		}
	case []byte:
		u.Meta.FileSize = int64(len(s))
	case fs.File:
		fi, err := s.Stat()
		if err != nil {
			return err
		}
		u.Meta.FileSize = fi.Size()
		u.Meta.FileName = fi.Name()
	case io.Reader:
		buff := bytes.NewBuffer([]byte{})
		fs, err := io.Copy(buff, s)
		if err != nil {
			return err
		}
		u.Meta.FileSize = fs
		u.Source = bytes.NewReader(buff.Bytes())
	}
	if u.ChunkSize == 0 {
		u.ChunkSize = DEFAULT_PARTS
	}
	if int64(u.ChunkSize) > u.Meta.FileSize {
		u.ChunkSize = int32(u.Meta.FileSize)
		u.Parts = 1
		u.Worker = 1
	}
	if u.Parts == 0 {
		u.Parts = int32(u.Meta.FileSize / int64(u.ChunkSize))
		if u.Meta.FileSize%int64(u.ChunkSize) != 0 {
			u.Parts++
		}
	}
	u.Worker = getInt(u.Worker, DEFAULT_WORKERS)
	if u.Meta.FileSize < 10*1024*1024 { // Less than 10MB - use small file upload
		u.Meta.Md5Hash = md5.New() //if file size is less than 10MB then we need to calculate md5 hash
	} else {
		u.Meta.IsBig = true
	}
	u.FileID = GenerateRandomLong() // Generate random file id
	u.wg = &sync.WaitGroup{}
	return nil
}

func (u *Uploader) allocateWorkers() error {
	borrowedSenders, err := u.Client.BorrowExportedSenders(u.Client.GetDC(), u.Worker)
	if err != nil {
		return err
	}
	u.Workers = borrowedSenders
	u.Client.Log.Info(fmt.Sprintf("Uploading file %s with %d workers", u.Meta.FileName, len(u.Workers)))

	u.Client.Log.Debug("Allocated workers: ", len(u.Workers), " for file upload (%s)", u.Meta.FileName)
	return nil
}

func (u *Uploader) saveFile() InputFile {
	if u.Meta.OpenFile != nil {
		u.Meta.OpenFile.Close()
	}
	if u.Meta.IsBig {
		return &InputFileBig{u.FileID, u.Parts, u.Meta.FileName}
	}
	return &InputFileObj{u.FileID, u.Parts, u.Meta.FileName, string(u.Meta.Md5Hash.Sum(nil))}
}

func (u *Uploader) dividePartsToWorkers() [][]int32 {
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
		parts = u.dividePartsToWorkers()
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
		if u.Meta.OpenFile == nil {
			u.Meta.OpenFile, err = os.Open(s)
			if err != nil {
				return nil, err
			}
		}
		offset := int64(part * u.ChunkSize)
		buf := make([]byte, u.ChunkSize)
		_, err = u.Meta.OpenFile.ReadAt(buf, offset)
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
	buf, _ := u.readPart(parts[0])
	for i := parts[0]; i < parts[1]; i++ {
		//_, err := u.readPart(i)
		var err error
		if err != nil {
			u.Client.Log.Error(err)
			continue
		}
		if u.Meta.IsBig {
			_, err = w.UploadSaveBigFilePart(u.FileID, i, u.Parts, buf)
		} else {
			u.Meta.Md5Hash.Write(buf)
			_, err = w.UploadSaveFilePart(u.FileID, i, buf)
		}

		w.Logger.Debug(fmt.Sprintf("uploaded part %d of %d", i, u.Parts))
		u.totalDone++
		if u.progress != nil {
			u.progress(u.Parts, u.totalDone)
		}
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
	location, dc, size, fileName, err := GetFileLocation(file)
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
		if err := os.MkdirAll(filepath.Dir(d.FileName), 0755); err != nil {
			return nil, err
		}
	}
	return os.Create(d.FileName)
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

func UploadProgressBar(m *NewMessage, total int32, now int32) {
	var (
		progressfillemojirectangleempty = "◾️"
		progressfillemojirectanglefull  = "◻️"
	)

	genPg := func(filled int32, total int32) string {
		var (
			empty = total - filled
		)
		var totalnumofprogressbar int32 = 10
		filled = filled / (total / totalnumofprogressbar)
		empty = totalnumofprogressbar - filled
		return fmt.Sprintf("%s%s", strings.Repeat(progressfillemojirectanglefull, int(filled)), strings.Repeat(progressfillemojirectangleempty, int(empty)))
	}
	if _, err := m.Edit(fmt.Sprintf("Uploading %s  %s", genPg(now, total), "p.String()")); err != nil {
		m.Reply(fmt.Sprintf("Error: %s", err.Error()))
	}
	// TODO: implement this
}
