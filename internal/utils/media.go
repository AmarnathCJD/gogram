package utils

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

type MP4Info struct {
	Duration  int64
	Width     uint32
	Height    uint32
	Timescale uint32
}

type MKVInfo struct {
	Duration float64
	Width    uint32
	Height   uint32
}

type AudioInfo struct {
	Duration   int64
	Bitrate    uint32
	Channels   uint32
	SampleRate uint32
}

func ParseMP4(filename string) (*MP4Info, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &MP4Info{}

	moovBox, err := findBox(f, "moov")
	if err != nil {
		return nil, fmt.Errorf("moov box not found: %w", err)
	}

	mvhdBox, err := findBoxInBox(f, moovBox, "mvhd")
	if err == nil {
		if err := parseMvhd(f, mvhdBox, info); err != nil {
			return nil, fmt.Errorf("failed to parse mvhd: %w", err)
		}
	}

	trakBox, err := findBoxInBox(f, moovBox, "trak")
	if err == nil {
		tkhdBox, err := findBoxInBox(f, trakBox, "tkhd")
		if err == nil {
			if err := parseTkhd(f, tkhdBox, info); err != nil {
				return nil, fmt.Errorf("failed to parse tkhd: %w", err)
			}
		}
	}

	if info.Timescale > 0 {
		info.Duration = (info.Duration * 1000) / int64(info.Timescale)
	}

	return info, nil
}

type boxInfo struct {
	offset  uint64
	size    uint64
	boxType string
}

func findBox(r io.ReadSeeker, targetType string) (*boxInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	for {
		box, err := readBoxHeader(r)
		if err == io.EOF {
			return nil, errors.New("box not found")
		}
		if err != nil {
			return nil, err
		}
		if box.boxType == targetType {
			return box, nil
		}
		if _, err := r.Seek(int64(box.offset+box.size), io.SeekStart); err != nil {
			return nil, err
		}
	}
}

func findBoxInBox(r io.ReadSeeker, parent *boxInfo, targetType string) (*boxInfo, error) {
	dataStart := parent.offset + 8
	if _, err := r.Seek(int64(dataStart), io.SeekStart); err != nil {
		return nil, err
	}
	parentEnd := parent.offset + parent.size
	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if uint64(currentPos) >= parentEnd {
			return nil, errors.New("box not found in parent")
		}
		box, err := readBoxHeader(r)
		if err == io.EOF {
			return nil, errors.New("box not found in parent")
		}
		if err != nil {
			return nil, err
		}
		if box.boxType == targetType {
			return box, nil
		}
		if _, err := r.Seek(int64(box.offset+box.size), io.SeekStart); err != nil {
			return nil, err
		}
	}
}

func readBoxHeader(r io.ReadSeeker) (*boxInfo, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	size := uint64(binary.BigEndian.Uint32(header[0:4]))
	boxType := string(header[4:8])
	if size == 1 {
		extSize := make([]byte, 8)
		if _, err := io.ReadFull(r, extSize); err != nil {
			return nil, err
		}
		size = binary.BigEndian.Uint64(extSize)
	}
	return &boxInfo{offset: uint64(offset), size: size, boxType: boxType}, nil
}

func parseMvhd(r io.ReadSeeker, box *boxInfo, info *MP4Info) error {
	if _, err := r.Seek(int64(box.offset+8), io.SeekStart); err != nil {
		return err
	}
	versionFlags := make([]byte, 4)
	if _, err := io.ReadFull(r, versionFlags); err != nil {
		return err
	}
	version := versionFlags[0]
	switch version {
	case 0:
		skip := make([]byte, 8)
		if _, err := io.ReadFull(r, skip); err != nil {
			return err
		}
		timescaleBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, timescaleBuf); err != nil {
			return err
		}
		info.Timescale = binary.BigEndian.Uint32(timescaleBuf)
		durationBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, durationBuf); err != nil {
			return err
		}
		info.Duration = int64(binary.BigEndian.Uint32(durationBuf))
	case 1:
		skip := make([]byte, 16)
		if _, err := io.ReadFull(r, skip); err != nil {
			return err
		}
		timescaleBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, timescaleBuf); err != nil {
			return err
		}
		info.Timescale = binary.BigEndian.Uint32(timescaleBuf)
		durationBuf := make([]byte, 8)
		if _, err := io.ReadFull(r, durationBuf); err != nil {
			return err
		}
		info.Duration = int64(binary.BigEndian.Uint64(durationBuf))
	}
	return nil
}

func parseTkhd(r io.ReadSeeker, box *boxInfo, info *MP4Info) error {
	if _, err := r.Seek(int64(box.offset+8), io.SeekStart); err != nil {
		return err
	}
	versionFlags := make([]byte, 4)
	if _, err := io.ReadFull(r, versionFlags); err != nil {
		return err
	}
	version := versionFlags[0]
	var skipBytes int
	if version == 0 {
		skipBytes = 20
	} else {
		skipBytes = 32
	}
	skipBytes += 52
	skip := make([]byte, skipBytes)
	if _, err := io.ReadFull(r, skip); err != nil {
		return err
	}
	widthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, widthBuf); err != nil {
		return err
	}
	widthFixed := binary.BigEndian.Uint32(widthBuf)
	info.Width = widthFixed >> 16
	heightBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, heightBuf); err != nil {
		return err
	}
	heightFixed := binary.BigEndian.Uint32(heightBuf)
	info.Height = heightFixed >> 16
	return nil
}

const (
	idEBML          = 0x1A45DFA3
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idTracks        = 0x1654AE6B
	idTrackEntry    = 0xAE
	idTrackType     = 0x83
	idVideo         = 0xE0
	idPixelWidth    = 0xB0
	idPixelHeight   = 0xBA
)

func ParseMKV(filename string) (*MKVInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &MKVInfo{}

	if err := verifyEBMLHeader(f); err != nil {
		return nil, fmt.Errorf("invalid MKV file: %w", err)
	}

	segmentDataOffset, segmentSize, err := findEBMLElementData(f, idSegment)
	if err != nil {
		return nil, fmt.Errorf("segment not found: %w", err)
	}

	_ = parseMKVInfo(f, segmentDataOffset, segmentSize, info)

	if err := parseMKVTracks(f, segmentDataOffset, segmentSize, info); err != nil {
		return nil, fmt.Errorf("failed to parse tracks: %w", err)
	}

	return info, nil
}

func verifyEBMLHeader(r io.ReadSeeker) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	elementID, _, err := readEBMLElementHeader(r)
	if err != nil {
		return err
	}
	if elementID != idEBML {
		return errors.New("not a valid EBML file")
	}
	return nil
}

func findEBMLElementData(r io.ReadSeeker, targetID uint32) (uint64, uint64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, 0, err
	}
	for {
		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			return 0, 0, errors.New("element not found")
		}
		if err != nil {
			return 0, 0, err
		}
		dataOffset, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, 0, err
		}
		if elementID == targetID {
			return uint64(dataOffset), size, nil
		}
		if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
			return 0, 0, err
		}
	}
}

func findEBMLElementInRange(r io.ReadSeeker, startOffset, rangeSize uint64, targetID uint32) (uint64, uint64, error) {
	if _, err := r.Seek(int64(startOffset), io.SeekStart); err != nil {
		return 0, 0, err
	}
	endOffset := startOffset + rangeSize
	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, 0, err
		}
		if uint64(currentPos) >= endOffset {
			return 0, 0, errors.New("element not found in range")
		}
		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			return 0, 0, errors.New("element not found in range")
		}
		if err != nil {
			return 0, 0, err
		}
		dataOffset, _ := r.Seek(0, io.SeekCurrent)
		if elementID == targetID {
			return uint64(dataOffset), size, nil
		}
		if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
			return 0, 0, err
		}
	}
}

func readEBMLElementHeader(r io.Reader) (uint32, uint64, error) {
	idByte, err := readByte(r)
	if err != nil {
		return 0, 0, err
	}
	var elementID uint32
	var idSize int
	if idByte&0x80 != 0 {
		idSize = 1
		elementID = uint32(idByte)
	} else if idByte&0x40 != 0 {
		idSize = 2
		elementID = uint32(idByte)
	} else if idByte&0x20 != 0 {
		idSize = 3
		elementID = uint32(idByte)
	} else if idByte&0x10 != 0 {
		idSize = 4
		elementID = uint32(idByte)
	} else {
		return 0, 0, errors.New("invalid EBML element ID")
	}
	for i := 1; i < idSize; i++ {
		b, err := readByte(r)
		if err != nil {
			return 0, 0, err
		}
		elementID = (elementID << 8) | uint32(b)
	}
	sizeByte, err := readByte(r)
	if err != nil {
		return 0, 0, err
	}
	var size uint64
	var sizeLength int
	switch {
	case sizeByte&0x80 != 0:
		sizeLength, size = 1, uint64(sizeByte&0x7F)
	case sizeByte&0x40 != 0:
		sizeLength, size = 2, uint64(sizeByte&0x3F)
	case sizeByte&0x20 != 0:
		sizeLength, size = 3, uint64(sizeByte&0x1F)
	case sizeByte&0x10 != 0:
		sizeLength, size = 4, uint64(sizeByte&0x0F)
	case sizeByte&0x08 != 0:
		sizeLength, size = 5, uint64(sizeByte&0x07)
	case sizeByte&0x04 != 0:
		sizeLength, size = 6, uint64(sizeByte&0x03)
	case sizeByte&0x02 != 0:
		sizeLength, size = 7, uint64(sizeByte&0x01)
	case sizeByte&0x01 != 0:
		sizeLength, size = 8, 0
	default:
		return 0, 0, errors.New("invalid EBML element size")
	}
	for i := 1; i < sizeLength; i++ {
		b, err := readByte(r)
		if err != nil {
			return 0, 0, err
		}
		size = (size << 8) | uint64(b)
	}
	return elementID, size, nil
}

func parseMKVInfo(r io.ReadSeeker, segmentOffset, segmentSize uint64, info *MKVInfo) error {
	infoOffset, infoSize, err := findEBMLElementInRange(r, segmentOffset, segmentSize, idInfo)
	if err != nil {
		return err
	}
	timecodeScale := uint64(1000000)
	if _, err := r.Seek(int64(infoOffset), io.SeekStart); err != nil {
		return err
	}
	infoEnd := infoOffset + infoSize
	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if uint64(currentPos) >= infoEnd {
			break
		}
		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch elementID {
		case idTimecodeScale:
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return err
			}
			timecodeScale = readUInt(data)
		case idDuration:
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return err
			}
			duration := readFloat(data)
			info.Duration = (duration * float64(timecodeScale)) / 1000000.0
		default:
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseMKVTracks(r io.ReadSeeker, segmentOffset, segmentSize uint64, info *MKVInfo) error {
	tracksOffset, tracksSize, err := findEBMLElementInRange(r, segmentOffset, segmentSize, idTracks)
	if err != nil {
		return err
	}
	if _, err := r.Seek(int64(tracksOffset), io.SeekStart); err != nil {
		return err
	}
	tracksEnd := tracksOffset + tracksSize
	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if uint64(currentPos) >= tracksEnd {
			break
		}
		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if elementID == idTrackEntry {
			if err := parseMKVTrackEntry(r, size, info); err == nil && info.Width > 0 {
				return nil
			}
		} else {
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseMKVTrackEntry(r io.ReadSeeker, trackEntrySize uint64, info *MKVInfo) error {
	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	trackEnd := uint64(startPos) + trackEntrySize
	isVideoTrack := false
	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if uint64(currentPos) >= trackEnd {
			break
		}
		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if elementID == idTrackType {
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return err
			}
			trackType := readUInt(data)
			if trackType == 1 {
				isVideoTrack = true
			}
		} else if elementID == idVideo && isVideoTrack {
			return parseMKVVideo(r, size, info)
		} else {
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return err
			}
		}
	}
	if !isVideoTrack {
		return errors.New("not a video track")
	}
	return nil
}

func parseMKVVideo(r io.ReadSeeker, videoSize uint64, info *MKVInfo) error {
	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	videoEnd := uint64(startPos) + videoSize

	for {
		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		if uint64(currentPos) >= videoEnd {
			break
		}

		elementID, size, err := readEBMLElementHeader(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		switch elementID {
		case idPixelWidth:
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return err
			}
			info.Width = uint32(readUInt(data))

		case idPixelHeight:
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return err
			}
			info.Height = uint32(readUInt(data))

		default:
			// Skip unknown elements
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return err
			}
		}
	}

	return nil
}

func readByte(r io.Reader) (byte, error) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func readUInt(data []byte) uint64 {
	var result uint64
	for _, b := range data {
		result = (result << 8) | uint64(b)
	}
	return result
}

// readFloat reads an EBML float (4 or 8 bytes)
func readFloat(data []byte) float64 {
	if len(data) == 4 {
		bits := binary.BigEndian.Uint32(data)
		return float64(math.Float32frombits(bits))
	} else if len(data) == 8 {
		bits := binary.BigEndian.Uint64(data)
		return math.Float64frombits(bits)
	}
	return 0
}

func ParseM4A(filename string) (*AudioInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mp4Info, err := parseMP4Internal(f)
	if err != nil {
		return nil, err
	}

	audioInfo := &AudioInfo{
		Duration: mp4Info.Duration,
	}

	moovBox, err := findBox(f, "moov")
	if err == nil {
		trakBox, err := findBoxInBox(f, moovBox, "trak")
		if err == nil {
			mdiaBox, err := findBoxInBox(f, trakBox, "mdia")
			if err == nil {
				mdhdBox, err := findBoxInBox(f, mdiaBox, "mdhd")
				if err == nil {
					parseMdhd(f, mdhdBox, audioInfo)
				}
			}
		}
	}

	return audioInfo, nil
}

func parseMP4Internal(f *os.File) (*MP4Info, error) {
	info := &MP4Info{}

	moovBox, err := findBox(f, "moov")
	if err != nil {
		return nil, fmt.Errorf("moov box not found: %w", err)
	}

	mvhdBox, err := findBoxInBox(f, moovBox, "mvhd")
	if err == nil {
		if err := parseMvhd(f, mvhdBox, info); err != nil {
			return nil, fmt.Errorf("failed to parse mvhd: %w", err)
		}
	}

	if info.Timescale > 0 {
		info.Duration = (info.Duration * 1000) / int64(info.Timescale)
	}

	return info, nil
}

func parseMdhd(r io.ReadSeeker, box *boxInfo, info *AudioInfo) error {
	if _, err := r.Seek(int64(box.offset+8), io.SeekStart); err != nil {
		return err
	}

	versionFlags := make([]byte, 4)
	if _, err := io.ReadFull(r, versionFlags); err != nil {
		return err
	}

	version := versionFlags[0]
	var skipBytes int
	if version == 0 {
		skipBytes = 12
	} else {
		skipBytes = 20
	}

	skip := make([]byte, skipBytes)
	if _, err := io.ReadFull(r, skip); err != nil {
		return err
	}

	sampleRateBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, sampleRateBuf); err != nil {
		return err
	}
	info.SampleRate = binary.BigEndian.Uint32(sampleRateBuf)

	return nil
}

var mp3Bitrates = [5][16]int{
	{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
}

var mp3Samplerates = [3][3]int{
	{44100, 48000, 32000},
	{22050, 24000, 16000},
	{11025, 12000, 8000},
}

func ParseMP3(filename string) (*AudioInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &AudioInfo{}

	id3Size := skipID3v2(f)
	if _, err := f.Seek(int64(id3Size), io.SeekStart); err != nil {
		return nil, err
	}

	header := make([]byte, 4)
	frameCount := 0
	totalBitrate := uint32(0)
	var fileSize int64

	stat, err := f.Stat()
	if err == nil {
		fileSize = stat.Size()
	}

	for frameCount < 100 {
		if _, err := io.ReadFull(f, header); err != nil {
			break
		}

		if header[0] != 0xFF || (header[1]&0xE0) != 0xE0 {
			if _, err := f.Seek(-3, io.SeekCurrent); err != nil {
				break
			}
			continue
		}

		version := (header[1] >> 3) & 0x03
		layer := (header[1] >> 1) & 0x03
		bitrateIndex := (header[2] >> 4) & 0x0F
		samplerateIndex := (header[2] >> 2) & 0x03
		padding := (header[2] >> 1) & 0x01

		if version == 1 || layer == 0 || bitrateIndex == 0 || bitrateIndex == 15 || samplerateIndex == 3 {
			if _, err := f.Seek(-3, io.SeekCurrent); err != nil {
				break
			}
			continue
		}

		var bitrateTable int
		if version == 3 {
			switch layer {
			case 3:
				bitrateTable = 0
			case 2:
				bitrateTable = 1
			default:
				bitrateTable = 2
			}
		} else {
			if layer == 3 {
				bitrateTable = 3
			} else {
				bitrateTable = 4
			}
		}

		bitrate := mp3Bitrates[bitrateTable][bitrateIndex]

		var samplerateTable int
		switch version {
		case 3:
			samplerateTable = 0
		case 2:
			samplerateTable = 1
		default:
			samplerateTable = 2
		}

		samplerate := mp3Samplerates[samplerateTable][samplerateIndex]

		if frameCount == 0 {
			info.Bitrate = uint32(bitrate)
			info.SampleRate = uint32(samplerate)
			if (header[3]>>6)&0x03 == 3 {
				info.Channels = 1
			} else {
				info.Channels = 2
			}
		}

		totalBitrate += uint32(bitrate)
		frameCount++

		var frameSize int
		if layer == 3 {
			frameSize = (144000*bitrate)/samplerate + int(padding)
		} else {
			frameSize = (144000*bitrate)/samplerate + int(padding)
		}

		if _, err := f.Seek(int64(frameSize-4), io.SeekCurrent); err != nil {
			break
		}
	}

	if frameCount > 0 {
		avgBitrate := totalBitrate / uint32(frameCount)
		if fileSize > 0 && avgBitrate > 0 {
			audioBytesSize := fileSize - int64(id3Size)
			info.Duration = (audioBytesSize * 8 * 1000) / (int64(avgBitrate) * 1000)
		}
	}

	return info, nil
}

func skipID3v2(r io.ReadSeeker) int {
	header := make([]byte, 10)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0
	}

	if string(header[0:3]) != "ID3" {
		r.Seek(0, io.SeekStart)
		return 0
	}

	size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
	return size + 10
}

func ParseWAV(filename string) (*AudioInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &AudioInfo{}

	riffHeader := make([]byte, 12)
	if _, err := io.ReadFull(f, riffHeader); err != nil {
		return nil, err
	}

	if string(riffHeader[0:4]) != "RIFF" || string(riffHeader[8:12]) != "WAVE" {
		return nil, errors.New("not a valid WAV file")
	}

	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			break
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		if chunkID == "fmt " {
			fmtData := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, fmtData); err != nil {
				return nil, err
			}

			if len(fmtData) >= 16 {
				info.Channels = uint32(binary.LittleEndian.Uint16(fmtData[2:4]))
				info.SampleRate = binary.LittleEndian.Uint32(fmtData[4:8])
				byteRate := binary.LittleEndian.Uint32(fmtData[8:12])
				info.Bitrate = (byteRate * 8) / 1000
			}
		} else if chunkID == "data" {
			byteRate := (info.SampleRate * info.Channels * info.Bitrate) / 8
			if byteRate > 0 {
				info.Duration = int64(chunkSize) * 1000 / int64(byteRate)
			}
			break
		} else {
			if _, err := f.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				break
			}
		}
	}

	return info, nil
}

func ParseFLAC(filename string) (*AudioInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &AudioInfo{}

	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, err
	}

	if string(header) != "fLaC" {
		return nil, errors.New("not a valid FLAC file")
	}

	for {
		blockHeader := make([]byte, 4)
		if _, err := io.ReadFull(f, blockHeader); err != nil {
			break
		}

		isLast := (blockHeader[0] & 0x80) != 0
		blockType := blockHeader[0] & 0x7F
		blockSize := int(blockHeader[1])<<16 | int(blockHeader[2])<<8 | int(blockHeader[3])

		if blockType == 0 {
			streamInfo := make([]byte, blockSize)
			if _, err := io.ReadFull(f, streamInfo); err != nil {
				return nil, err
			}

			if len(streamInfo) >= 18 {
				info.SampleRate = (uint32(streamInfo[10])<<12 | uint32(streamInfo[11])<<4 | uint32(streamInfo[12])>>4)
				info.Channels = ((uint32(streamInfo[12]) >> 1) & 0x07) + 1
				bitsPerSample := ((uint32(streamInfo[12]) & 0x01) << 4) | (uint32(streamInfo[13]) >> 4)

				totalSamples := uint64(streamInfo[13]&0x0F)<<32 |
					uint64(streamInfo[14])<<24 |
					uint64(streamInfo[15])<<16 |
					uint64(streamInfo[16])<<8 |
					uint64(streamInfo[17])

				if info.SampleRate > 0 {
					info.Duration = int64(totalSamples) * 1000 / int64(info.SampleRate)
					info.Bitrate = uint32((bitsPerSample * info.Channels * info.SampleRate) / 1000)
				}
			}
			break
		}

		if isLast {
			break
		}

		if _, err := f.Seek(int64(blockSize), io.SeekCurrent); err != nil {
			break
		}
	}

	return info, nil
}

func ParseWebM(filename string) (*MKVInfo, error) {
	return ParseMKV(filename)
}
