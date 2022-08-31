// Copyright (c) 2022, amarnathcjd

package telegram

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"github.com/pkg/errors"
)

// UploadFile is a function that uploads a file to telegram as byte chunks
// and returns the InputFile object
// File can be Path to file, or a byte array
func (c *Client) UploadFile(file interface{}) (InputFile, error) {
	var (
		fileName   string
		fileSize   int64
		totalParts int32
		Index      int32
		bigFile    bool
		chunkSize  int
		reader     *bufio.Reader
		fileID     = GenerateRandomLong()
		hasher     = md5.New()
	)
	switch f := file.(type) {
	case string:
		fileSize, fileName = getFileStat(f)
		if fileSize == 0 {
			return nil, errors.New("file not found")
		}
		chunkSize = getAppropriatedPartSize(fileSize)
		totalParts = int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
		bigFile = fileSize > 10*1024*1024
		fileBytes, err := os.Open(f)
		if err != nil {
			return nil, errors.Wrap(err, "opening file")
		}
		defer fileBytes.Close()
		reader = bufio.NewReader(fileBytes)
	case []byte:
		fileSize = int64(len(f))
		chunkSize = getAppropriatedPartSize(fileSize)
		totalParts = int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
		bigFile = fileSize > 10*1024*1024
		reader = bufio.NewReaderSize(bytes.NewReader(f), chunkSize)
	case io.Reader:
	default:
		return nil, errors.New("invalid file type")
	}
	buffer := make([]byte, chunkSize)
	log.Println("Client - INFO - Uploading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
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
// FileDL can be MessageMedia, FileLocation
func (c *Client) DownloadMedia(FileDL interface{}, DownloadPath ...string) (string, error) {
	var (
		fileLocation InputFileLocation
		fileSize     int64
	)
	fileLocation, _, fileSize = GetInputFileLocation(FileDL)
	if fileLocation == nil {
		return "", errors.New("invalid file type")
	}
	chunkSize := getAppropriatedPartSize(fileSize)
	totalParts := int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
	var (
		fileName string
		file     *os.File
		err      error
	)
	if len(DownloadPath) > 0 {
		fileName = DownloadPath[0]
	} else {
		fileName = getValue(getFileName(FileDL), "download").(string)
	}
	if isPathDirectoryLike(fileName) {
		fileName = fileName + "/" + getFileName(FileDL)
		os.MkdirAll(fileName, os.ModePerm)
	}
	file, err = os.Create(fileName)
	if err != nil {
		return "", errors.Wrap(err, "creating file")
	}
	defer file.Close()
	log.Println("Client - INFO - Downloading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
	for i := int32(0); i < totalParts; i++ {
		fileData, err := c.UploadGetFile(&UploadGetFileParams{
			Precise:      false,
			CdnSupported: false,
			Location:     fileLocation,
			Offset:       int64(i * int32(chunkSize)),
			Limit:        int32(chunkSize),
		})
		if err != nil {
			return "", errors.Wrap(err, "downloading file")
		}
		switch f := fileData.(type) {
		case *UploadFileObj:
			_, err = file.Write(f.Bytes)
			if err != nil {
				return "", errors.Wrap(err, "writing file")
			}
		case *UploadFileCdnRedirect:
			return "", errors.New(fmt.Sprintf("File lives on another DC (%d), Not supported yet", f.DcID))
		}
	}
	return fileName, nil
}
