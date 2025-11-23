package main

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"
)

var appId = 6 // fill in your app id here

func main() {
	path := "./tdata" //"<path-to-tdata-folder>" // (eg:- C:\\Users\\user\\Roaming\\Telegram Desktop\\tdata\\)
	tdata := NewTData(path)

	sessions, err := tdata.toGogram()
	if err != nil {
		panic(err)
	}

	for _, x := range sessions {
		client, err := telegram.NewClient(telegram.ClientConfig{
			StringSession: x.Encode(),
			MemorySession: true,
			AppID:         int32(appId),
		})

		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(client.GetMe())
	}
}

var MAGIC = "TDF$"

type TData struct {
	path string
}

type TDF struct {
	Version       uint32
	EncryptedData []byte
	Hashsum       []byte
}

func NewTData(path string) *TData {
	return &TData{path: path}
}

func readTD(f *os.File) (*TDF, error) {
	buf := make([]byte, len(MAGIC))
	_, err := f.Read(buf)
	if err != nil {
		return nil, err
	}

	if string(buf) != MAGIC {
		return nil, fmt.Errorf("invalid magic number: not a tdata file")
	}

	var data []byte
	for {
		buf := make([]byte, 1024)
		n, err := f.Read(buf)
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		data = append(data, buf[:n]...)
	}
	var tdf = TDF{
		Version:       binary.LittleEndian.Uint32(data[:4]),
		EncryptedData: data[4 : len(data)-16],
		Hashsum:       data[len(data)-16:],
	}

	var checksum = make([]byte, len(tdf.Hashsum))
	copy(checksum, tdf.Hashsum)

	if string(checksum) != string(calculateChecksum(tdf)) {
		return nil, fmt.Errorf("checksum mismatch")
	}

	return &tdf, nil
}

type MtpData struct {
	UserID uint64
	User32 int32
	MainDC int32
	Keys   []struct {
		DC      int32
		AuthKey [256]byte
	}
}

func (t *TData) toGogram() ([]*telegram.Session, error) {
	mtpDatas, err := t.readKeyFile()
	if err != nil {
		return nil, err
	}

	var sessions []*telegram.Session
	for _, mtpData := range mtpDatas {
		maindc := mtpData.MainDC
		var maindcAuthKey []byte
		for _, key := range mtpData.Keys {
			if key.DC == maindc {
				maindcAuthKey = key.AuthKey[:256]
				break
			}
		}
		sessions = append(sessions, &telegram.Session{
			Key:      maindcAuthKey,
			Hostname: telegram.ResolveDataCenterIP(int(maindc), false, false),
		})
	}

	return sessions, nil
}

func (t *TData) readKeyFile() ([]*MtpData, error) {
	keysFile := filepath.Join(t.path, "key_datas")
	f, err := os.Open(keysFile)
	if err != nil {
		f, err = os.Open(filepath.Join(t.path, "key_data"))
		if err != nil {
			return nil, fmt.Errorf("key_data(s) file not found in tdata folder")
		}
	}

	tdf, err := readTD(f)
	r := bytes.NewReader(tdf.EncryptedData)

	var length uint32
	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}

	var salt = make([]byte, length)
	r.Read(salt)

	binary.Read(r, binary.BigEndian, &length)
	var keySealed = make([]byte, length)
	r.Read(keySealed)

	binary.Read(r, binary.BigEndian, &length)
	var infoSealed = make([]byte, length)
	r.Read(infoSealed)

	key := createLocalKey(salt)
	localKey, err := decryptLocal(keySealed, key)
	if err != nil {
		return nil, err
	}
	index, err := decryptLocal(infoSealed, localKey)
	if err != nil {
		return nil, err
	}

	var indexLength uint32
	r = bytes.NewReader(index)
	err = binary.Read(r, binary.BigEndian, &indexLength)
	if err != nil {
		return nil, err
	}
	var indexes []int
	for i := 0; i < int(indexLength); i++ {
		var index uint32
		err = binary.Read(r, binary.BigEndian, &index)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, int(index))
	}
	var mainAccount uint32
	err = binary.Read(r, binary.BigEndian, &mainAccount)
	if err != nil {
		return nil, err
	}

	f.Close()
	var mtpDatas []*MtpData

	for _, index := range indexes {
		cmpName := "data#" + fmt.Sprintf("%d", index+1)
		if index == int(mainAccount) {
			cmpName = "data"
		}

		key := md5.Sum([]byte(cmpName))
		key_ := key[:8]
		var result strings.Builder
		for _, b := range key_ {
			hexString := fmt.Sprintf("%X", b)
			reversedHex := reverseString(hexString)
			result.WriteString(reversedHex)
		}

		f, err := os.Open(filepath.Join(t.path, result.String()+"s"))
		if err != nil {
			if os.IsNotExist(err) {
				f, err = os.Open(filepath.Join(t.path, result.String()))
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}

		tdf, err := readTD(f)
		if err != nil {
			return nil, err
		}

		data, err := decryptLocal(tdf.EncryptedData[4:], localKey)
		if err != nil {
			return nil, err
		}

		var r = bytes.NewReader(data)

		for {
			var fieldId uint32
			err = binary.Read(r, binary.BigEndian, &fieldId)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if fieldId == 0x4b {
				var length uint32
				err = binary.Read(r, binary.BigEndian, &length)
				if err != nil {
					return nil, err
				}
				var data = make([]byte, length)
				r.Read(data)

				var mtpData = MtpData{}
				r = bytes.NewReader(data)
				binary.Read(r, binary.BigEndian, &mtpData.User32)
				binary.Read(r, binary.BigEndian, &mtpData.MainDC)
				if mtpData.User32 == -1 && mtpData.MainDC == -1 {
					binary.Read(r, binary.BigEndian, &mtpData.UserID)
					binary.Read(r, binary.BigEndian, &mtpData.MainDC)
				}

				var i int32
				binary.Read(r, binary.BigEndian, &i)
				for j := 0; j < int(i); j++ {
					var dcId int32
					binary.Read(r, binary.BigEndian, &dcId)
					var authKey [256]byte
					r.Read(authKey[:])
					mtpData.Keys = append(mtpData.Keys, struct {
						DC      int32
						AuthKey [256]byte
					}{dcId, authKey})
				}

				mtpDatas = append(mtpDatas, &mtpData)
			}
		}
	}

	return mtpDatas, nil
}

func createLocalKey(salt []byte) []byte {
	iterations := 1

	hash := sha512.New()
	hash.Write(salt)
	hash.Write([]byte(""))
	hash.Write(salt)
	password := hash.Sum(nil)

	key := PBKDF2(password, salt, iterations, 256)
	return key
}

func decryptLocal(data, localKey []byte) ([]byte, error) {
	key, data := data[:16], data[16:]
	decrypted, err := aesDecryptLocal(data, key, localKey)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	hash := sha1.Sum(decrypted)

	if !bytes.Equal(hash[:16], key) {
		return nil, errors.New("bad decrypt key, data not decrypted - incorrect password")
	}

	length := binary.LittleEndian.Uint32(decrypted[:4])

	if int(length) > len(decrypted) {
		return nil, fmt.Errorf("corrupted data. wrong length: %d", length)
	}

	return decrypted[4:length], nil
}

func aesDecryptLocal(data, dataKey, localKey []byte) ([]byte, error) {
	key, iv := keysToAES(dataKey, localKey)
	return IGEDecrypt(key, iv, data), nil
}

func keysToAES(dataKey, localKey []byte) ([]byte, []byte) {
	x := 8

	keyPos := func(pos, size int) []byte {
		slice := make([]byte, size)
		copy(slice, localKey[pos:pos+size])
		return slice
	}

	dataA := make([]byte, len(dataKey)+32)
	copy(dataA, dataKey)
	copy(dataA[len(dataKey):], keyPos(x, 32))

	dataB := make([]byte, 16+len(dataKey)+16)
	copy(dataB, keyPos(x+32, 16))
	copy(dataB[16:], dataKey)
	copy(dataB[16+len(dataKey):], keyPos(x+48, 16))

	dataC := make([]byte, 32+len(dataKey))
	copy(dataC, keyPos(x+64, 32))
	copy(dataC[32:], dataKey)

	dataD := make([]byte, len(dataKey)+32)
	copy(dataD, dataKey)
	copy(dataD[len(dataKey):], keyPos(x+96, 32))

	sha1A := sha1.Sum(dataA)
	sha1B := sha1.Sum(dataB)
	sha1C := sha1.Sum(dataC)
	sha1D := sha1.Sum(dataD)

	key := make([]byte, 8+12+12)
	copy(key, sha1A[:8])
	copy(key[8:], sha1B[8:20])
	copy(key[20:], sha1C[4:16])

	iv := make([]byte, 12+8+4+8)
	copy(iv, sha1A[8:20])
	copy(iv[12:], sha1B[:8])
	copy(iv[20:], sha1C[16:20])
	copy(iv[24:], sha1D[:8])

	return key, iv
}

func calculateChecksum(tdf TDF) []byte {
	encryptedDataLen := make([]byte, 4)
	versionBytes := make([]byte, 4)

	binary.LittleEndian.PutUint32(encryptedDataLen, uint32(len(tdf.EncryptedData)))
	binary.LittleEndian.PutUint32(versionBytes, tdf.Version)

	data := append(tdf.EncryptedData, encryptedDataLen...)
	data = append(data, versionBytes...)
	data = append(data, MAGIC...)
	checksum := md5.Sum(data)

	return checksum[:]
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// ----------------- AES IGE -----------------

func IGEDecrypt(key, iv, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	if len(iv) != block.BlockSize()*2 {
		panic("IV length must equal 2 * block size")
	}

	blockSize := block.BlockSize()
	decrypted := make([]byte, len(data))

	xPrev := iv[:blockSize]
	yPrev := iv[blockSize:]

	for i := 0; i < len(data); i += blockSize {
		blockData := data[i : i+blockSize]
		temp := make([]byte, blockSize)

		for j := 0; j < blockSize; j++ {
			temp[j] = blockData[j] ^ yPrev[j]
		}

		block.Decrypt(temp, temp)
		for j := 0; j < blockSize; j++ {
			decrypted[i+j] = temp[j] ^ xPrev[j]
		}
		xPrev = blockData
		yPrev = decrypted[i : i+blockSize]
	}

	return decrypted
}

func PBKDF2(password, salt []byte, iterations, keyLen int) []byte {
	hashFunc := sha512.New
	hLen := hashFunc().Size()

	if keyLen <= 0 || iterations <= 0 {
		panic("invalid key length or iterations")
	}

	numBlocks := (keyLen + hLen - 1) / hLen
	output := make([]byte, keyLen)

	for i := 1; i <= numBlocks; i++ {
		block := F(password, salt, iterations, i, hashFunc)
		start := (i - 1) * hLen
		end := start + hLen
		if end > keyLen {
			end = keyLen
		}
		copy(output[start:end], block[:end-start])
	}

	return output
}

func F(password, salt []byte, iterations, blockNum int, hashFunc func() hash.Hash) []byte {
	mac := hmac.New(hashFunc, password)
	mac.Write(append(salt, byte(blockNum>>24), byte(blockNum>>16), byte(blockNum>>8), byte(blockNum)))
	u := mac.Sum(nil)

	result := make([]byte, len(u))
	copy(result, u)

	for i := 1; i < iterations; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range result {
			result[j] ^= u[j]
		}
	}

	return result
}
