package telegram

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"log"
	"math"
	"os"
	"sync"

	"github.com/pkg/errors"
)

// UploadFile is a function that uploads a file to telegram as byte chunks
// and returns the InputFile object.
func (c *Client) UploadFile(filePath string) (InputFile, error) {
	fileSize, fileName := getFileStat(filePath)
	if fileSize == 0 {
		return nil, errors.New("file not found")
	}
	hasher := md5.New()
	fileID := GenerateRandomLong()
	chunkSize := getAppropriatedPartSize(fileSize)
	totalParts := int32(math.Ceil(float64(fileSize) / float64(chunkSize)))
	BigFile := fileSize > 10*1024*1024
	var Index int32
	file, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "opening file")
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	buffer := make([]byte, chunkSize)
	log.Println("Client - INFO - Uploading file", fileName, "with", totalParts, "parts of", chunkSize, "bytes")
	for {
		n, err := reader.Read(buffer)
		if err != nil {
			break
		}
		if BigFile {
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
	if BigFile {
		return &InputFileBig{
			ID:    fileID,
			Name:  fileName,
			Parts: totalParts,
		}, nil
	} else {
		return &InputFileObj{
			ID:          fileID,
			Parts:       Index,
			Name:        fileName,
			Md5Checksum: fmt.Sprintf("%x", hasher.Sum(nil)),
		}, nil
	}
}

// Soon
func SaveFile(fileObj *os.File, chunkSize int, numgoroutines int, c *Client, fileID int64, index int, totalParts int, big bool) {
	parts := totalParts / numgoroutines
	if totalParts%numgoroutines != 0 {
		parts++
	}
	fmt.Println("Started saving file", fileID, "with", totalParts, "parts")
	chunks := make(chan []byte, parts)
	var wg sync.WaitGroup
	for i := 0; i < numgoroutines; i++ {
		wg.Add(1)
		fmt.Println("Started saving file", fileID, "with", totalParts, "parts")
		go func(i int) {
			defer wg.Done()
			for range chunks {
				fmt.Println("Uploading part", i*parts+index)
			}
		}(i)
	}
	x, _ := bufio.NewReader(fileObj).ReadBytes('\n')
	genByteChunks(x, chunkSize, chunks)
	wg.Wait()
}
