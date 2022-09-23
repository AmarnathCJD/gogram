// Copyright (c) 2022, amarnathcjd

package telegram

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
)

// UploadFile is a function that uploads a file to telegram as byte chunks
// and returns the InputFile object
// File can be Path to file, or a byte array
func (c *Client) UploadFile(file interface{}, Opts ...*UploadOptions) (InputFile, error) {
	var (
		opts       *UploadOptions
		fileName   string
		fileSize   int64
		totalParts int32
		Index      int32
		bigFile    bool
		chunkSize  int
		reader     *bufio.Reader
		fileID     = GenerateRandomLong()
		hasher     = md5.New()
		fileBytes  *os.File
		Prog       *Progress
	)
	if len(Opts) > 0 {
		opts = Opts[0]
		Prog = opts.Progress
		if Prog == nil {
			Prog = &Progress{}
		}
	} else {
		opts = &UploadOptions{}
		Prog = &Progress{}
	}
	switch f := file.(type) {
	case string:
		fileSize, fileName = getFileStat(f)
		if fileSize == 0 {
			return nil, errors.New("file not found")
		}
		chunkSize = getAppropriatedPartSize(fileSize)
		totalParts = int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
		bigFile = fileSize > 10*1024*1024 // 10MB
		fileBytes, err := os.Open(f)
		if err != nil {
			return nil, errors.Wrap(err, "opening file")
		}
		defer fileBytes.Close()
		reader = bufio.NewReader(fileBytes)
	case []byte: // TODO: Add support for goroutines for byte array
		fileSize = int64(len(f))
		chunkSize = getAppropriatedPartSize(fileSize)
		totalParts = int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
		bigFile = fileSize > 10*1024*1024
		reader = bufio.NewReaderSize(bytes.NewReader(f), chunkSize)
	case InputFile:
		return f, nil // already an Uploaded file
	default:
		return nil, errors.New("invalid file type")
	}
	Prog.Total = int64(totalParts)
	log.Println("Client - INFO - Uploading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
	if opts.Threaded && bigFile {
		return c.uploadBigMultiThread(fileName, int(fileSize), fileID, fileBytes, int32(chunkSize), totalParts, opts.Progress, opts.Threads)
	}
	buffer := make([]byte, chunkSize)
	for {
		n, err := reader.Read(buffer)
		if err != nil {
			break
		}
		if bigFile {
			_, err = c.UploadSaveBigFilePart(fileID, Index, totalParts, buffer[:n])
		} else {
			hasher.Write(buffer[:n])
			_, err = c.UploadSaveFilePart(fileID, Index, buffer[:n])
		}
		if err != nil {
			return nil, errors.Wrap(err, "uploading file")
		}
		Index++
	}
	if bigFile {
		return &InputFileBig{ID: fileID, Name: fileName, Parts: totalParts}, nil
	}
	return &InputFileObj{ID: fileID, Parts: Index, Name: fileName, Md5Checksum: fmt.Sprintf("%x", hasher.Sum(nil))}, nil
}

// DownloadMedia is a function that downloads a media file from telegram,
// and returns the file path,
// FileDL can be MessageMedia, FileLocation, NewMessage
func (c *Client) DownloadMedia(FileDL interface{}, DLOptions ...*DownloadOptions) (string, error) {
	var (
		fileLocation InputFileLocation
		fileSize     int64
		DcID         int32
		Opts         *DownloadOptions
		Prog         *Progress
	)
	if len(DLOptions) > 0 {
		Opts = DLOptions[0]
	} else {
		Opts = &DownloadOptions{}
	}
	switch f := FileDL.(type) {
	case *MessageMediaContact:
		file, err := os.Create(getValue(Opts.FileName, f.PhoneNumber+".contact").(string))
		if err != nil {
			return "", errors.Wrap(err, "creating file")
		}
		defer file.Close()
		vcard_4 := fmt.Sprintf("BEGIN:VCARD\nVERSION:4.0\nN:%s;%s;;;\nFN:%s %s\nTEL;TYPE=CELL:%s\nEND:VCARD", f.LastName, f.FirstName, f.FirstName, f.LastName, f.PhoneNumber)
		_, err = file.WriteString(vcard_4)
		if err != nil {
			return "", errors.Wrap(err, "writing to file")
		}
		return file.Name(), nil
	default:
		fileLocation, DcID, fileSize = GetInputFileLocation(FileDL)
	}
	if fileLocation == nil {
		return "", errors.New("could not get file location: " + fmt.Sprintf("%T", FileDL))
	}
	if fileSize == 0 {
		fileSize = 622
	}
	if DcID == 0 {
		DcID = Opts.DcID
	}
	chunkSize := getAppropriatedPartSize(fileSize)
	totalParts := int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
	if Opts.Progress != nil {
		Prog = Opts.Progress
	}
	Prog.Total = int64(totalParts)
	var (
		fileName string
		file     *os.File
		err      error
	)
	if Opts.FileName != "" {
		fileName = Opts.FileName
	} else {
		fileName = getValue(GetFileName(FileDL), "download").(string)
	}
	if isPathDirectoryLike(fileName) {
		fileName = filepath.Join(fileName, GetFileName(FileDL))
		os.MkdirAll(fileName, os.ModePerm)
	}
	file, err = os.Create(fileName)
	if err != nil {
		return "", errors.Wrap(err, "creating file")
	}
	defer file.Close()

	log.Println("Client - INFO - Downloading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
	if Opts.Threaded {
		return fileName, c.downloadBigMultiThread(file, fileLocation, totalParts, int32(chunkSize), DcID, Prog, Opts.Threads)
	}
	if DcID != int32(c.GetDC()) {
		c.Logger.Println("Client - INFO - File Lives on DC", DcID, ", Exporting Sender...")
		s, _ := c.ExportSender(int(DcID))
		defer s.Terminate()
		_, err = getFile(s, fileLocation, file, int32(chunkSize), totalParts, Prog)
		if err != nil {
			return "", errors.Wrap(err, "downloading file")
		}
	} else {
		_, err = getFile(c, fileLocation, file, int32(chunkSize), totalParts, Prog)
		if err != nil {
			return "", errors.Wrap(err, "downloading file")
		}
	}
	return fileName, nil
}

func (c *Client) DownloadProfilePhoto(PeerID interface{}, Pfp interface{}, DLOptions ...*DownloadOptions) (string, error) {
	var (
		Opts *DownloadOptions
		Prog *Progress
	)
	if len(DLOptions) > 0 {
		Opts = DLOptions[0]
	} else {
		Opts = &DownloadOptions{}
	}
	if Opts.FileName == "" {
		Opts.FileName = "profile_photo.jpg"
	}
	location, dcID, fileSize, err := c.getPeerPhotoLocation(PeerID, Pfp)
	Opts.Size = fileSize
	if err != nil {
		return "", errors.Wrap(err, "getting photo location")
	}
	if location == nil {
		return "", errors.New("could not get photo location")
	}
	return c.DownloadMedia(location, &DownloadOptions{DcID: dcID, Progress: Prog, FileName: Opts.FileName})
}

func (c *Client) uploadBigMultiThread(fileName string, fileSize int, fileID int64, fileBytes *os.File, chunkSize int32, totalParts int32, prog *Progress, threadCount int) (*InputFileBig, error) {
	if threadCount <= 0 {
		threadCount = 20
	}
	if totalParts < int32(threadCount) {
		threadCount = int(totalParts)
	}
	partsAllocation := MultiThreadAllocation(int32(chunkSize), totalParts, threadCount)
	senders := make([]*Client, threadCount)
	wg := sync.WaitGroup{}
	for i := 0; i < threadCount; i++ {
		wg.Add(1)
		go func(ix int) {
			defer wg.Done()
			senders[ix], _ = c.ExportSender(c.GetDC())
		}(i)
	}
	wg.Wait()
	log.Println("Client - INFO - Uploading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
	for i := 0; i < threadCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := partsAllocation[i][0]; j < partsAllocation[i][1]; j++ {
				buffer := make([]byte, chunkSize)
				_, _ = fileBytes.ReadAt(buffer, int64(j*int32(chunkSize)))
				_, err := senders[i].UploadSaveBigFilePart(fileID, j, totalParts, buffer)
				if prog != nil {
					prog.Add(1)
				}
				if err != nil {
					log.Println("Error in uploading", err)
				}
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < threadCount; i++ {
		senders[i].Terminate()
	}
	return &InputFileBig{
		ID:    fileID,
		Name:  fileName,
		Parts: totalParts,
	}, nil
}

func (c *Client) downloadBigMultiThread(file *os.File, fileLocation InputFileLocation, totalParts int32, chunkSize int32, dcID int32, prog *Progress, ThreadCount int) error {
	if ThreadCount <= 0 {
		ThreadCount = 20
	}
	if totalParts < int32(ThreadCount) {
		ThreadCount = int(totalParts)
	}
	partsAllocation := MultiThreadAllocation(int32(chunkSize), totalParts, ThreadCount)
	senders := make([]*Client, ThreadCount)
	wg := sync.WaitGroup{}
	for i := 0; i < ThreadCount; i++ {
		wg.Add(1)
		go func(ix int) {
			defer wg.Done()
			senders[ix], _ = c.ExportSender(int(dcID))
		}(i)
	}
	wg.Wait()
	for i := 0; i < ThreadCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := partsAllocation[i][0]; j < partsAllocation[i][1]; j++ {
				b, err := senders[i].getFilePart(fileLocation, chunkSize, j)
				if err != nil {
					log.Println("Error in downloading", err)
				}
				prog.Add(1)
				_, _ = file.WriteAt(b, int64(j*int32(chunkSize)))
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < ThreadCount; i++ {
		senders[i].Terminate()
	}
	return nil
}

func (c *Client) getFilePart(l InputFileLocation, chunkSize int32, Offset int32) ([]byte, error) {
	var (
		filePart []byte
		err      error
	)
	request, err := c.UploadGetFile(&UploadGetFileParams{
		Precise:      false,
		CdnSupported: false,
		Location:     l,
		Offset:       int64(Offset * chunkSize),
		Limit:        chunkSize,
	})
	if err != nil {
		return nil, errors.Wrap(err, "getting file part")
	}
	switch r := request.(type) {
	case *UploadFileCdnRedirect:
		// filePart, err = c.getFilePartCdnRedirect(r, chunkSize, Offset)
		// if err != nil {
		//	return nil, errors.Wrap(err, "getting file part")
		// }
		return nil, errors.New("cdn redirect not supported yet")
	case *UploadFileObj:
		filePart = r.Bytes
	}
	return filePart, nil
}

func getFile(c *Client, location InputFileLocation, f *os.File, chunkSize int32, totalParts int32, progress *Progress) (bool, error) {
	for i := int32(0); i < totalParts; i++ {
		fileData, err := c.UploadGetFile(&UploadGetFileParams{
			Precise:      false,
			CdnSupported: false,
			Location:     location,
			Offset:       int64(i * int32(chunkSize)),
			Limit:        chunkSize,
		})
		if progress != nil {
			progress.Set(int64(i))
		}

		if err != nil {
			if strings.Contains(err.Error(), "The file to be accessed is currently stored in DC") {
				dcID := regexp.MustCompile(`\d+`).FindString(err.Error())
				return false, errors.New("INVALID_DC_" + dcID)
			}
			return false, errors.Wrap(err, "downloading file")
		}
		switch file := fileData.(type) {
		case *UploadFileObj:
			_, err = f.Write(file.Bytes)
			if err != nil {
				return false, errors.Wrap(err, "writing file")
			}
		case *UploadFileCdnRedirect:
			return false, errors.New("CDN_REDIRECT: Not implemented yet")
		}
	}
	return true, nil
}

func MultiThreadAllocation(chunkSize int32, totalParts int32, numGorotines int) map[int][]int32 {
	partsForEachGoRoutine := int32(math.Ceil(float64(totalParts) / float64(numGorotines)))
	remainingParts := totalParts
	partsAllocation := make(map[int][]int32, numGorotines)
	for i := 0; i < numGorotines; i++ {
		if remainingParts > partsForEachGoRoutine {
			partsAllocation[i] = []int32{int32(i) * partsForEachGoRoutine, (int32(i) + 1) * partsForEachGoRoutine}
			remainingParts -= partsForEachGoRoutine
		} else {
			partsAllocation[i] = []int32{int32(i) * partsForEachGoRoutine, totalParts}
		}
	}
	return partsAllocation
}

func (c *Client) getPeerPhotoLocation(PeerID interface{}, Photo interface{}) (*InputPeerPhotoFileLocation, int32, int32, error) {
	var (
		peer InputPeer
		err  error
	)
	peer, err = c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, 0, 0, errors.Wrap(err, "getting peer")
	}
	var location *InputPeerPhotoFileLocation
	var dcID int32
	var fileSize int32
PfpTypeSwitch:
	switch pfp := Photo.(type) {
	case *UserProfilePhotoObj:
		location = &InputPeerPhotoFileLocation{
			PhotoID: pfp.PhotoID,
			Peer:    peer,
			Big:     true,
		}
		dcID = pfp.DcID
		fileSize = int32(len(pfp.StrippedThumb))
	case *ChatPhotoObj:
		location = &InputPeerPhotoFileLocation{
			PhotoID: pfp.PhotoID,
			Peer:    peer,
			Big:     true,
		}
		dcID = pfp.DcID
		fileSize = int32(len(pfp.StrippedThumb))
	case *UserObj:
		switch pfp.Photo.(type) {
		case *UserProfilePhotoObj:
			goto PfpTypeSwitch
		default:
			return nil, 0, 0, errors.New("user has no profile photo")
		}
	case *ChatObj:
		switch pfp.Photo.(type) {
		case *ChatPhotoObj:
			goto PfpTypeSwitch
		default:
			return nil, 0, 0, errors.New("chat has no profile photo")
		}
	case *Channel:
		switch pfp.Photo.(type) {
		case *ChatPhotoObj:
			goto PfpTypeSwitch
		default:
			return nil, 0, 0, errors.New("channel has no profile photo")
		}
	default:
		return nil, 0, 0, errors.New("invalid profile photo type")
	}
	return location, dcID, fileSize, nil
}
