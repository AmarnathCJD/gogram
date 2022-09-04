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
	"regexp"
	"strings"

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
		DcID         int32
	)
	fileLocation, DcID, fileSize = GetInputFileLocation(FileDL) // TODO: Fix fileSize for Photos
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
	if DcID != int32(c.GetDC()) {
		c.Logger.Println("Client - INFO - File Lives on DC", DcID, ", Exporting Sender...")
		s, _ := c.ExportSender(int(DcID))
		_, err = getFile(s, fileLocation, file, int32(chunkSize), totalParts)
		s.Terminate()
		if err != nil {
			return "", errors.Wrap(err, "downloading file")
		}
	} else {
		_, err = getFile(c, fileLocation, file, int32(chunkSize), totalParts)
		if err != nil {
			return "", errors.Wrap(err, "downloading file")
		}
	}
	return fileName, nil
}

func getFile(c *Client, location InputFileLocation, f *os.File, chunkSize int32, totalParts int32) (bool, error) {
	for i := int32(0); i < totalParts; i++ {
		fileData, err := c.UploadGetFile(&UploadGetFileParams{
			Precise:      false,
			CdnSupported: false,
			Location:     location,
			Offset:       int64(i * int32(chunkSize)),
			Limit:        chunkSize,
		})
		if err != nil {
			if strings.Contains(err.Error(), "The file to be accessed is currently stored in DC") {
				dcID := regexp.MustCompile(`\d+`).FindString(err.Error())
				fmt.Println("wrong DC", dcID)
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
			return false, errors.New("File lives on another DC (%d), Not supported yet")
		}
	}
	return true, nil
}

// TODO
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
